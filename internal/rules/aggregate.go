package rules

import (
	"context"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Aggregator 同仓同类事件短时合并，抑制通知风暴。
type Aggregator struct {
	Store          store.Store
	Window         time.Duration
	BurstThreshold int
	BurstWindow    time.Duration

	mu sync.Mutex
	// key: repoID|category
	buckets map[string]*aggBucket
	// burst: repoID -> timestamps
	bursts map[string][]time.Time
}

type aggBucket struct {
	category string
	repoID   string
	repoName string
	events   []*store.Event
	timer    *time.Timer
}

// NewAggregator 创建聚合器。
func NewAggregator(st store.Store, window time.Duration, burstThreshold int, burstWindow time.Duration) *Aggregator {
	if window <= 0 {
		window = 60 * time.Second
	}
	if burstThreshold <= 0 {
		burstThreshold = 15
	}
	if burstWindow <= 0 {
		burstWindow = 5 * time.Minute
	}
	return &Aggregator{
		Store: st, Window: window, BurstThreshold: burstThreshold, BurstWindow: burstWindow,
		buckets: make(map[string]*aggBucket), bursts: make(map[string][]time.Time),
	}
}

// ReloadFrom 从 system_settings 热加载聚合参数；未设置或非法的键保留当前值。
// 管理台修改 notify.* 后调用，参数即时生效而无需重启。
func (a *Aggregator) ReloadFrom(ctx context.Context) error {
	window, err1 := readPositiveIntSetting(ctx, a.Store, "notify.aggregate_window_sec")
	threshold, err2 := readPositiveIntSetting(ctx, a.Store, "notify.burst_threshold")
	burstWindow, err3 := readPositiveIntSetting(ctx, a.Store, "notify.burst_window_sec")
	if err1 != nil && err2 != nil && err3 != nil {
		return err1
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if window > 0 {
		a.Window = time.Duration(window) * time.Second
	}
	if threshold > 0 {
		a.BurstThreshold = threshold
	}
	if burstWindow > 0 {
		a.BurstWindow = time.Duration(burstWindow) * time.Second
	}
	return nil
}

// readPositiveIntSetting 读取整型设置；键不存在或值非法时返回 0。
func readPositiveIntSetting(ctx context.Context, st store.Store, key string) (int, error) {
	row, err := st.Settings().Get(ctx, key)
	if err != nil {
		return 0, err
	}
	var v float64
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return 0, err
	}
	n := int(v)
	if float64(n) != v || n <= 0 {
		return 0, nil
	}
	return n, nil
}

func categoryOf(ev *store.Event) string {
	switch ev.Kind {
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		return "security"
	case "workflow_run":
		return "actions"
	case store.WorkItemKindIssue:
		return "issue"
	case store.WorkItemKindPR:
		return "pr"
	default:
		return "other"
	}
}

// Evaluate 带聚合的通知决策。
func (a *Aggregator) Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error {
	if res.Event == nil || res.SuppressNotify || res.Event.SuppressNotification {
		return nil
	}
	// 能力开关兜底：全局 + 仓库级；聚合窗口期间开关变化也不能漏拦。
	if !allowsEventKind(ctx, a.Store, res.Repository, res.Event.Kind) {
		return nil
	}
	if !shouldNotifyRealtime(res.Event) {
		return nil
	}
	repoID := ""
	if res.Event.RepositoryID != nil {
		repoID = *res.Event.RepositoryID
	}
	cat := categoryOf(res.Event)
	key := repoID + "|" + cat

	a.mu.Lock()
	now := time.Now()
	// 超频检测
	times := a.bursts[repoID]
	filtered := times[:0]
	for _, t := range times {
		if now.Sub(t) <= a.BurstWindow {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	a.bursts[repoID] = filtered
	if len(filtered) > a.BurstThreshold {
		sample := res.Event
		title := "⚠️ 通知频率超限"
		a.mu.Unlock()
		// 降级：只写一条速率限制摘要（必须在锁外访问 Store）
		return a.enqueueBurstSummary(ctx, repoID, repoFullName, cat, title, sample)
	}

	b, ok := a.buckets[key]
	if !ok {
		b = &aggBucket{category: cat, repoID: repoID, repoName: repoFullName, events: []*store.Event{res.Event}}
		a.buckets[key] = b
		b.timer = time.AfterFunc(a.Window, func() {
			a.flush(key)
		})
		a.mu.Unlock()
		return nil
	}
	b.events = append(b.events, res.Event)
	a.mu.Unlock()
	return nil
}

func (a *Aggregator) flush(key string) {
	a.mu.Lock()
	b, ok := a.buckets[key]
	if !ok {
		a.mu.Unlock()
		return
	}
	delete(a.buckets, key)
	a.mu.Unlock()

	ctx := context.Background()
	if len(b.events) == 1 {
		_ = (&Engine{Store: a.Store}).Evaluate(ctx, normalizer.Result{Event: b.events[0]}, b.repoName)
		return
	}
	_ = a.enqueueMerged(ctx, b)
}

// enqueueMerged 按渠道订阅过滤桶内事件子集，逐渠道构建合并消息。
func (a *Aggregator) enqueueMerged(ctx context.Context, b *aggBucket) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	// 窗口结束时再按最新全局功能开关过滤：聚合窗口期间被关闭的类型不得漏发。
	features := store.LoadFeatureFlags(ctx, a.Store.Settings())
	// 多实例：幂等键按「渠道 + 仓 + 类别 + 时间桶」稳定，避免各副本各写一条合并通知。
	// 进程内合并仍是 best-effort；跨实例重复事件靠 Outbox 唯一约束收敛。
	bucket := timeBucket(time.Now().UTC(), a.Window)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		sub := make([]*store.Event, 0, len(b.events))
		for _, ev := range b.events {
			if ch.AcceptsKind(ev.Kind) && features.AllowsKind(ev.Kind) {
				sub = append(sub, ev)
			}
		}
		if len(sub) == 0 {
			continue
		}
		title, body := renderMergedMessage(b.repoName, b.category, sub)
		variant := fmt.Sprintf("agg|%s|%s|%d", b.repoID, b.category, bucket)
		idem := idempotencyKey(ch.ID, b.repoID, variant)
		eventID := sub[0].ID
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eventID,
			AggregateKey: b.repoID + "|" + b.category, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: map[string]any{"aggregate": true, "count": len(sub), "category": b.category, "bucket": bucket},
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
}

// renderMergedMessage 由事件子集构建合并标题与正文（对 Telegram HTML 做转义）。
func renderMergedMessage(repoName, category string, events []*store.Event) (string, string) {
	categoryCN := categoryDisplayName(category)
	title := fmt.Sprintf("📋 %s：%s × %d（已合并）", htmlpkg.EscapeString(repoName), htmlpkg.EscapeString(categoryCN), len(events))
	var body strings.Builder
	body.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	body.WriteString("────────────────\n")
	maxSamples := 8
	for i, ev := range events {
		if i >= maxSamples {
			body.WriteString(fmt.Sprintf("…另有 %d 条\n", len(events)-maxSamples))
			break
		}
		emoji := eventEmoji(ev)
		numStr := ""
		if ev.SubjectNumber != nil {
			numStr = fmt.Sprintf(" #%d", *ev.SubjectNumber)
		}
		stateStr := ""
		if ev.Action != "" {
			stateStr = fmt.Sprintf("（%s）", htmlpkg.EscapeString(ev.Action))
		}
		body.WriteString(fmt.Sprintf("%s%s %s%s\n", emoji, numStr, htmlpkg.EscapeString(ev.Title), stateStr))
	}
	return title, body.String()
}

func (a *Aggregator) enqueueBurstSummary(ctx context.Context, repoID, repoName, cat, title string, sample *store.Event) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	bucket := timeBucket(time.Now().UTC(), a.BurstWindow)
	categoryCN := categoryDisplayName(cat)
	safeTitle := htmlpkg.EscapeString(title)
	safeRepo := htmlpkg.EscapeString(repoName)
	safeCat := htmlpkg.EscapeString(categoryCN)
	body := fmt.Sprintf("<b>%s</b>\n────────────────\n📦 仓库：<code>%s</code>\n📋 类型：%s\n🔇 已降级为摘要模式，请在仪表盘查看详情", safeTitle, safeRepo, safeCat)
	for _, ch := range channels {
		// 以 sample 事件的类型判定渠道是否接收超频摘要。
		if !ch.Enabled || !ch.AcceptsKind(sample.Kind) {
			continue
		}
		variant := fmt.Sprintf("burst|%s|%s|%d", repoID, cat, bucket)
		idem := idempotencyKey(ch.ID, repoID, variant)
		eid := sample.ID
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eid,
			AggregateKey: repoID + "|burst", IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: safeTitle, BodyText: body, ParseMode: "HTML",
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
}

// timeBucket 将时间对齐到窗口边界，供多实例 Outbox 幂等键使用。
func timeBucket(t time.Time, window time.Duration) int64 {
	if window <= 0 {
		window = 60 * time.Second
	}
	// 亚秒窗口按毫秒对齐，避免 int64(window/time.Second)==0 除零。
	if window < time.Second {
		ms := window.Milliseconds()
		if ms <= 0 {
			ms = 1
		}
		return t.UnixMilli() / ms
	}
	sec := int64(window / time.Second)
	if sec <= 0 {
		sec = 1
	}
	return t.Unix() / sec
}

// categoryDisplayName 返回类别的显示名（与前端标签一致）。
func categoryDisplayName(cat string) string {
	switch cat {
	case "security":
		return "安全告警"
	case "actions":
		return "Actions"
	case "issue":
		return "Issue"
	case "pr":
		return "PR"
	default:
		return cat
	}
}
