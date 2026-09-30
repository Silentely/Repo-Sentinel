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
	if _, err := data.Outbox().MarkSent(ctx, "ob-old-sent"); err != nil {
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

// TestDeleteTerminalOlderThanOnlyTerminalStatuses 锁定清理口径：无论 created_at 多旧，
// 只有终态（sent / dead / cancelled）会被删除；在途的 pending / sending 必须保留，
// 否则清理任务会把尚未投递的通知删掉造成静默丢消息。cutoff 取未来时刻以覆盖全部行。
func TestDeleteTerminalOlderThanOnlyTerminalStatuses(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	if _, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-retention", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "1",
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	rows := []struct {
		id     string
		status string
		keep   bool
	}{
		{"ob-pending", store.OutboxPending, true},
		{"ob-sending", store.OutboxSending, true},
		{"ob-sent", store.OutboxSent, false},
		{"ob-dead", store.OutboxDead, false},
		{"ob-cancelled", store.OutboxCancelled, false},
	}
	for _, row := range rows {
		if _, err := data.Outbox().Create(ctx, store.NotificationOutbox{
			ID:             row.id,
			ChannelID:      "ch-retention",
			IdempotencyKey: "idem-" + row.id,
			Status:         row.status,
			Title:          "row " + row.id,
			BodyText:       "body",
			NextAttemptAt:  now,
		}); err != nil {
			t.Fatalf("create outbox %s: %v", row.id, err)
		}
	}

	deleted, err := data.Outbox().DeleteTerminalOlderThan(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("delete terminal outbox: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("应只删除 3 条终态行，got %d", deleted)
	}

	items, page, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("应保留 2 条在途行，got total=%d items=%v", page.Total, items)
	}
	for _, item := range items {
		if item.Status != store.OutboxPending && item.Status != store.OutboxSending {
			t.Fatalf("在途行被误删或状态漂移: %+v", item)
		}
	}
}

// TestCleanupTransientSettings_ChatOps 过期的 ChatOps 令牌与陈旧领取标记被清理，
// 未过期令牌、新领取标记与无关设置键必须保留。
func TestCleanupTransientSettings_ChatOps(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	// 未过期令牌：保留
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-chatops-alive",
		Key:       store.ChatOpsTokenKey("alive"),
		ValueJSON: []byte(`{"expires_at":"` + now.Add(time.Hour).Format(time.RFC3339) + `"}`),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed alive token: %v", err)
	}
	// 已过期令牌：清理
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-chatops-dead",
		Key:       store.ChatOpsTokenKey("dead"),
		ValueJSON: []byte(`{"expires_at":"` + now.Add(-time.Minute).Format(time.RFC3339) + `"}`),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed expired token: %v", err)
	}
	// 解析失败的令牌：按失效处理并清理
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-chatops-corrupt",
		Key:       store.ChatOpsTokenKey("corrupt"),
		ValueJSON: []byte(`{"not_a_time_field":true}`),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed corrupt token: %v", err)
	}
	// 过期领取标记（updated_at 早于 1 小时保留窗口）：清理
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-chatops-stale-claim",
		Key:       store.ChatOpsClaimKey("stale-claim"),
		ValueJSON: []byte(`{"claimed":true}`),
		UpdatedAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed stale claim: %v", err)
	}
	// 新领取标记：保留
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-chatops-fresh-claim",
		Key:       store.ChatOpsClaimKey("fresh-claim"),
		ValueJSON: []byte(`{"claimed":true}`),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed fresh claim: %v", err)
	}
	// 无关设置键：不得触碰
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-feature-issues",
		Key:       "feature.issues",
		ValueJSON: []byte(`true`),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed unrelated key: %v", err)
	}

	deleted, err := data.CleanupTransientSettings(ctx, now)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deletions (expired + corrupt + stale claim), got %d", deleted)
	}

	for _, key := range []string{store.ChatOpsTokenKey("alive"), store.ChatOpsClaimKey("fresh-claim"), "feature.issues"} {
		if _, err := data.Settings().Get(ctx, key); err != nil {
			t.Fatalf("键 %s 不应被清理: %v", key, err)
		}
	}
	for _, key := range []string{store.ChatOpsTokenKey("dead"), store.ChatOpsTokenKey("corrupt"), store.ChatOpsClaimKey("stale-claim")} {
		if _, err := data.Settings().Get(ctx, key); err == nil {
			t.Fatalf("键 %s 应已被清理", key)
		}
	}
}
