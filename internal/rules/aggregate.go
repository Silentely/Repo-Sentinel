package rules

import (
	"context"
	"fmt"
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

	mu    sync.Mutex
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
	defer a.mu.Unlock()
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
		// 降级：只写一条速率限制摘要
		return a.enqueueSummaryLocked(ctx, repoID, repoFullName, cat, fmt.Sprintf("%s：通知过于频繁，已降级为摘要", repoFullName), res.Event)
	}

	b, ok := a.buckets[key]
	if !ok {
		b = &aggBucket{category: cat, repoID: repoID, repoName: repoFullName, events: []*store.Event{res.Event}}
		a.buckets[key] = b
		b.timer = time.AfterFunc(a.Window, func() {
			a.flush(key)
		})
		return nil
	}
	b.events = append(b.events, res.Event)
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
	// 合并消息
	title := fmt.Sprintf("📋 %s：%s %d 条（已合并）", b.repoName, b.category, len(b.events))
	var body string
	body = title + "\n"
	maxSamples := 5
	for i, ev := range b.events {
		if i >= maxSamples {
			body += fmt.Sprintf("…另有 %d 条\n", len(b.events)-maxSamples)
			break
		}
		body += fmt.Sprintf("• %s / %s — %s\n", ev.Kind, ev.Action, ev.Title)
		if ev.HTMLURL != "" {
			body += fmt.Sprintf("  %s\n", ev.HTMLURL)
		}
	}
	_ = a.enqueueMerged(ctx, b, title, body)
}

func (a *Aggregator) enqueueMerged(ctx context.Context, b *aggBucket, title, body string) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	// 用首条事件 id 参与幂等变体
	firstID := b.events[0].ID
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		idem := idempotencyKey(ch.ID, firstID, fmt.Sprintf("agg-%s-%d", b.category, len(b.events)))
		eventID := firstID
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eventID,
			AggregateKey: b.repoID + "|" + b.category, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: map[string]any{"aggregate": true, "count": len(b.events), "category": b.category},
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
}

func (a *Aggregator) enqueueSummaryLocked(ctx context.Context, repoID, repoName, cat, title string, sample *store.Event) error {
	// 调用方已持锁；释放后写库
	a.mu.Unlock()
	defer a.mu.Lock()
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		idem := idempotencyKey(ch.ID, sample.ID, "burst-"+cat)
		eid := sample.ID
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eid,
			AggregateKey: repoID + "|burst", IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: title + "\n仓库：" + repoName, ParseMode: "HTML",
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
}
