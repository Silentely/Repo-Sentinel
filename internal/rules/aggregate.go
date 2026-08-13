package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
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
	// AI 可选；flush 单事件回放时透传给 Engine 供安全告警分诊。
	AI *ai.Client
	// Logger 可选；透传给 Engine 供分诊参与度留痕。
	Logger *slog.Logger

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
	n, ok := store.CoerceInt(v)
	if !ok || n <= 0 {
		return 0, nil
	}
	return n, nil
}

func categoryOf(ev *store.Event) string {
	switch ev.Kind {
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		return "security"
	case store.WorkflowRunKind:
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
		// 标题带上仓库名：Telegram 推送预览只看标题，无仓库名时无法区分是哪个仓超频。
		// repoFullName 为空（如聚合器单事件回放）时回退通用标题；写入前统一转义。
		title := "⚠️ 通知频率超限"
		if repoFullName != "" {
			title = fmt.Sprintf("⚠️ 通知频率超限：%s", repoFullName)
		}
		a.mu.Unlock()
		// 降级：只写一条速率限制摘要（必须在锁外访问 Store）
		if a.Logger != nil {
			// 超频是异常流量信号：Warn 留痕便于审计与告警，摘要本身也会通知用户。
			a.Logger.Warn("burst summary enqueued",
				"repo", repoFullName, "category", cat, "events_in_window", len(filtered))
		}
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if len(b.events) == 1 {
		// 单事件回放：走实时通知评估。失败留痕，避免聚合窗口内的通知静默丢失。
		if err := (&Engine{Store: a.Store, AI: a.AI, Logger: a.Logger}).Evaluate(ctx, normalizer.Result{Event: b.events[0]}, b.repoName); err != nil && a.Logger != nil {
			a.Logger.Warn("aggregate flush evaluate failed",
				"repo", b.repoName, "category", b.category, "events", len(b.events), "error_code", "aggregate_flush_failed", "error", err.Error())
		}
		return
	}
	if a.Logger != nil {
		// 合并投递留痕：多事件合并为一条，便于按合并量评估聚合窗口配置是否合理。
		a.Logger.Debug("aggregate flushed",
			"repo", b.repoName, "category", b.category, "events", len(b.events))
	}
	if err := a.enqueueMerged(ctx, b); err != nil && a.Logger != nil {
		// 合并通知写库失败：与单事件回放同样留痕，DB 抖动时不再静默丢聚合通知。
		a.Logger.Warn("aggregate flush enqueue failed",
			"repo", b.repoName, "category", b.category, "events", len(b.events), "error_code", "aggregate_flush_failed", "error", err.Error())
	}
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
		title, body := renderMergedMessage(b.repoName, b.category, sub, time.Now().UTC())
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
		if err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	}
	return nil
}

// renderMergedMessage 由事件子集构建合并标题与正文（对 Telegram HTML 做转义）。
// windowEnd 为合并窗口结束时刻，正文标注批次时间便于用户判断通知对应的窗口。
func renderMergedMessage(repoName, category string, events []*store.Event, windowEnd time.Time) (string, string) {
	categoryCN := categoryDisplayName(category)
	// 「已聚合」而非「已合并」：避免与 PR 的「已合并」状态语义混淆
	//（同一条通知里既可能包含已合并的 PR，也可能表示本通知是聚合产物）。
	title := fmt.Sprintf("📋 %s：%s × %d（已聚合）", htmlpkg.EscapeString(repoName), htmlpkg.EscapeString(categoryCN), len(events))
	var body strings.Builder
	body.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	body.WriteString("────────────────\n")
	maxSamples := 8
	for i, ev := range events {
		if i >= maxSamples {
			body.WriteString(fmt.Sprintf("…另有 %d 条\n", len(events)-maxSamples))
			break
		}
		statusEmoji, statusLabel := statusDisplay(ev)
		numStr := ""
		if ev.SubjectNumber != nil {
			numStr = fmt.Sprintf(" #%d", *ev.SubjectNumber)
		}
		// 状态中文放标题前，合并列表同样一眼可读。
		body.WriteString(fmt.Sprintf("%s [%s]%s %s\n", statusEmoji, htmlpkg.EscapeString(statusLabel), numStr, htmlpkg.EscapeString(ev.Title)))
	}
	if !windowEnd.IsZero() {
		body.WriteString("────────────────\n")
		body.WriteString(fmt.Sprintf("⏰ 时间：%s\n", windowEnd.UTC().Format("2006-01-02 15:04 UTC")))
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
	now := time.Now().UTC()
	safeTitle := htmlpkg.EscapeString(title)
	safeRepo := htmlpkg.EscapeString(repoName)
	safeCat := htmlpkg.EscapeString(categoryCN)
	body := fmt.Sprintf("<b>%s</b>\n────────────────\n📦 仓库：<code>%s</code>\n📋 类型：%s\n🔇 已降级为摘要模式，请在仪表盘查看详情\n⏰ 时间：%s", safeTitle, safeRepo, safeCat, now.Format("2006-01-02 15:04 UTC"))
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
			// 有事件链接时附带跳转按钮，用户可从摘要直达原始事件。
			HTMLURL: sample.HTMLURL,
		})
		if err != nil && !errors.Is(err, store.ErrConflict) {
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
