package notify

import (
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func seedOutboxStatus(t *testing.T, st store.Store, id, status string) {
	t.Helper()
	if _, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: id, ChannelID: "ch-metric", IdempotencyKey: "idem|" + id,
		Status: status, NextAttemptAt: time.Now().UTC().Add(-time.Minute),
		Title: "t", BodyText: "b",
	}); err != nil {
		t.Fatalf("创建 outbox %s 失败: %v", id, err)
	}
}

func outboxStatusByID(t *testing.T, st store.Store, id string) string {
	t.Helper()
	rows, _, err := st.Outbox().List(t.Context(), store.ListFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row.Status
		}
	}
	t.Fatalf("outbox %s 不存在", id)
	return ""
}

// TestMarkDeadMetricOnlyOnPersistedTransition 回归：OnDead 指标只在该行真的落库为 dead 时触发。
// 守卫拒绝（行已被并发投递推进到终态）时 MarkDead 返回 (false, nil)，只按 error 判定会把
// 这种情况当成落库成功而自增死信计数，排障时出现「死信计数有值但列表查不到」的矛盾。
func TestMarkDeadMetricOnlyOnPersistedTransition(t *testing.T) {
	st := openWorkerTestStore(t)
	ctx := t.Context()

	deadFired := 0
	w := &Worker{Store: st, OnDead: func() { deadFired++ }}

	// 在途行：正常推进为死信并计一次。
	seedOutboxStatus(t, st, "ob-metric-inflight", store.OutboxSending)
	w.markDead(ctx, "ob-metric-inflight", "probe_dead")
	if deadFired != 1 {
		t.Fatalf("在途行转死信应触发一次 OnDead，实际 %d", deadFired)
	}
	if got := outboxStatusByID(t, st, "ob-metric-inflight"); got != store.OutboxDead {
		t.Fatalf("在途行应落库为 dead，实际 %q", got)
	}

	// 已 sent 行：守卫拒绝，不得计指标、不得改状态。
	seedOutboxStatus(t, st, "ob-metric-sent", store.OutboxSent)
	w.markDead(ctx, "ob-metric-sent", "probe_dead")
	if deadFired != 1 {
		t.Fatalf("守卫拒绝不得触发 OnDead，实际 %d", deadFired)
	}
	if got := outboxStatusByID(t, st, "ob-metric-sent"); got != store.OutboxSent {
		t.Fatalf("sent 行被改判为 %q", got)
	}
}
