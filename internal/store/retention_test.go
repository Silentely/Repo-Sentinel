package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestCleanupRetentionDeletesExpiredHistory(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)

	if _, err := data.Events().Create(ctx, store.Event{
		ID: "evt-old", Source: "system", Kind: "issue", Action: "opened",
		Title: "old", OccurredAt: old, DedupeFingerprint: "fp-old", CreatedAt: old,
	}); err != nil {
		t.Fatalf("create old event: %v", err)
	}
	if _, err := data.Events().Create(ctx, store.Event{
		ID: "evt-new", Source: "system", Kind: "issue", Action: "opened",
		Title: "new", OccurredAt: recent, DedupeFingerprint: "fp-new", CreatedAt: recent,
	}); err != nil {
		t.Fatalf("create new event: %v", err)
	}

	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-1", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if _, err := data.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "ob-old-sent", ChannelID: ch.ID, IdempotencyKey: "ob-old-sent",
		Status: store.OutboxSent, Title: "old sent", BodyText: "x", NextAttemptAt: old,
	}); err != nil {
		t.Fatalf("create old sent outbox: %v", err)
	}
	// Create 后会强制写 now；用底层接口再补一条并通过 MarkSent，再靠 DeleteTerminalOlderThan 的 cutoff 测。
	// 为可控时间，直接再造 delivery 与 event 即可；outbox 用 DeleteTerminalOlderThan 单独测。
	if _, err := data.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID: "wd-old", DeliveryID: "d-old", EventType: "issues", Status: store.DeliveryProcessed,
		ReceivedAt: old,
	}); err != nil {
		t.Fatalf("create old delivery: %v", err)
	}
	if _, err := data.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID: "wd-new", DeliveryID: "d-new", EventType: "issues", Status: store.DeliveryProcessed,
		ReceivedAt: recent,
	}); err != nil {
		t.Fatalf("create new delivery: %v", err)
	}

	// 先单独验证 outbox 删除：创建后 MarkSent，再 DeleteTerminalOlderThan 用未来 cutoff 应删掉。
	if err := data.Outbox().MarkSent(ctx, "ob-old-sent"); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	result, err := data.CleanupRetention(ctx, store.RetentionPolicy{
		EventsDays:            30,
		OutboxDays:            0, // outbox 另测
		WebhookDeliveriesDays: 30,
	}, now)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("events deleted=%d, want 1", result.EventsDeleted)
	}
	if result.WebhookDeliveriesDeleted != 1 {
		t.Fatalf("deliveries deleted=%d, want 1", result.WebhookDeliveriesDeleted)
	}

	events, page, err := data.Events().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if page.Total != 1 || len(events) != 1 || events[0].ID != "evt-new" {
		t.Fatalf("expected only evt-new, got total=%d items=%v", page.Total, events)
	}

	// outbox：用极大 cutoff（未来）删除所有终态
	n, err := data.Outbox().DeleteTerminalOlderThan(ctx, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("delete outbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("outbox deleted=%d, want 1", n)
	}

	// days=0 跳过
	result2, err := data.CleanupRetention(ctx, store.RetentionPolicy{}, now)
	if err != nil {
		t.Fatalf("empty policy cleanup: %v", err)
	}
	if result2.EventsDeleted != 0 || result2.OutboxDeleted != 0 || result2.WebhookDeliveriesDeleted != 0 {
		t.Fatalf("empty policy should delete nothing, got %+v", result2)
	}
}

func TestDefaultRetentionPolicy(t *testing.T) {
	p := store.DefaultRetentionPolicy()
	if p.EventsDays != 90 || p.OutboxDays != 30 || p.WebhookDeliveriesDays != 30 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}
