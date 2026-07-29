package rules

import (
	"context"
	"fmt"
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
	if !shouldNotifyRealtime(res.Event) {
		return nil
	}
	// 系统级关键：不合并（当前事件模型无独立 system kind 实时路径）
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
		title := fmt.Sprintf("%s：通知过于频繁，已降级为摘要", repoFullName)
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
	// 合并消息（标题/正文对 Telegram HTML 做转义）
	title := fmt.Sprintf("📋 %s：%s %d 条（已合并）", htmlEscape(b.repoName), htmlEscape(b.category), len(b.events))
	var body strings.Builder
	body.WriteString(title)
	body.WriteByte('\n')
	maxSamples := 5
	for i, ev := range b.events {
		if i >= maxSamples {
			body.WriteString(fmt.Sprintf("…另有 %d 条\n", len(b.events)-maxSamples))
			break
		}
		body.WriteString(fmt.Sprintf("• %s / %s — %s\n", htmlEscape(ev.Kind), htmlEscape(ev.Action), htmlEscape(ev.Title)))
		if ev.HTMLURL != "" {
			body.WriteString(fmt.Sprintf("  %s\n", htmlEscape(ev.HTMLURL)))
		}
	}
	_ = a.enqueueMerged(ctx, b, title, body.String())
}

func (a *Aggregator) enqueueMerged(ctx context.Context, b *aggBucket, title, body string) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	// 多实例：幂等键按「渠道 + 仓 + 类别 + 时间桶」稳定，避免各副本各写一条合并通知。
	// 进程内合并仍是 best-effort；跨实例重复事件靠 Outbox 唯一约束收敛。
	firstID := b.events[0].ID
	bucket := timeBucket(time.Now().UTC(), a.Window)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		variant := fmt.Sprintf("agg|%s|%s|%d", b.repoID, b.category, bucket)
		idem := idempotencyKey(ch.ID, b.repoID, variant)
		eventID := firstID
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eventID,
			AggregateKey: b.repoID + "|" + b.category, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: map[string]any{"aggregate": true, "count": len(b.events), "category": b.category, "bucket": bucket},
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
}

func (a *Aggregator) enqueueBurstSummary(ctx context.Context, repoID, repoName, cat, title string, sample *store.Event) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	bucket := timeBucket(time.Now().UTC(), a.BurstWindow)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		variant := fmt.Sprintf("burst|%s|%s|%d", repoID, cat, bucket)
		idem := idempotencyKey(ch.ID, repoID, variant)
		eid := sample.ID
		safeTitle := htmlEscape(title)
		safeRepo := htmlEscape(repoName)
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eid,
			AggregateKey: repoID + "|burst", IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: safeTitle, BodyText: safeTitle + "\n仓库：" + safeRepo, ParseMode: "HTML",
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

func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return replacer.Replace(s)
}
