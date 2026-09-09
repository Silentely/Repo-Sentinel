package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestClaimDueDoesNotStarveOnLockedItems 验证处于 sending 且未过期的锁定行不会占用 limit 配额，
// 避免队头锁定行导致后续就绪任务发生饥饿（SQL 条件下推守护）。
func TestClaimDueDoesNotStarveOnLockedItems(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-claim-test", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// 1. 创建一条已被锁定的任务（锁定到未来 5 分钟）
	lockedUntil := now.Add(5 * time.Minute)
	if _, err := data.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "ob-locked-1", ChannelID: ch.ID, IdempotencyKey: "idem-locked-1",
		Status: store.OutboxSending, NextAttemptAt: now.Add(-10 * time.Second),
		LockedUntil: &lockedUntil, Title: "locked", BodyText: "locked",
	}); err != nil {
		t.Fatalf("create locked outbox: %v", err)
	}

	// 2. 创建一条已就绪的任务
	if _, err := data.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "ob-ready-2", ChannelID: ch.ID, IdempotencyKey: "idem-ready-2",
		Status: store.OutboxPending, NextAttemptAt: now.Add(-5 * time.Second),
		Title: "ready", BodyText: "ready",
	}); err != nil {
		t.Fatalf("create ready outbox: %v", err)
	}

	// 3. 以 limit=1 领取任务：
	// 若未下推 locked_until 过滤条件，SQL 会优先拉取 ob-locked-1 并在内存中过滤掉，导致返回空（饥饿）；
	// 修复后 SQL 应直接跳过锁定的行，成功领取到 ob-ready-2。
	items, err := data.Outbox().ClaimDue(ctx, now, 2*time.Minute, 1)
	if err != nil {
		t.Fatalf("ClaimDue failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 claimed item, got %d (starvation occurred!)", len(items))
	}
	if items[0].ID != "ob-ready-2" {
		t.Fatalf("expected claimed item ob-ready-2, got %s", items[0].ID)
	}
	if items[0].Status != store.OutboxSending {
		t.Fatalf("expected status %s, got %s", store.OutboxSending, items[0].Status)
	}
}
