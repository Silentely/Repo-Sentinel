package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func openOutboxProbeStore(t *testing.T) Store {
	t.Helper()
	data, err := Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "ob.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func seedOutbox(t *testing.T, data Store, id, status string) {
	t.Helper()
	if _, err := data.Outbox().Create(t.Context(), NotificationOutbox{
		ID: id, ChannelID: "ch-1", Status: status, Title: "probe",
		IdempotencyKey: "probe-" + id,
	}); err != nil {
		t.Fatalf("seed outbox %s: %v", id, err)
	}
}

// TestMarkSentCannotRevertTerminalState 回归：终态标记必须带状态守卫。
// 原实现 MarkSent/MarkRetry/MarkDead 均按 ID 无条件更新，租约过期导致的两份在途投递
// 会让迟到的失败标记把已 sent 行改回 pending 再次投递，或让迟到的成功标记把 dead 行
// 改成 sent——而同文件的 RetryDead/RetryAllDead 都带 StatusEQ(OutboxDead) 守卫，
// 同类控制强弱不一致。
func TestMarkSentCannotRevertTerminalState(t *testing.T) {
	data := openOutboxProbeStore(t)
	ctx := t.Context()

	// sent 行不得被 MarkRetry 拉回 pending。
	seedOutbox(t, data, "ob-sent", OutboxSent)
	if transitioned, err := data.Outbox().MarkRetry(ctx, "ob-sent", time.Now().UTC().Add(time.Minute), "probe"); err != nil {
		t.Fatalf("MarkRetry on sent 行应静默无操作而非报错: %v", err)
	} else if transitioned {
		t.Fatal("MarkRetry on sent 行不应报告状态已推进")
	}
	rows, _, err := data.Outbox().List(ctx, ListFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		if row.ID == "ob-sent" && row.Status != OutboxSent {
			t.Fatalf("sent 行被拉回 %q", row.Status)
		}
	}

	// dead 行不得被 MarkSent 改成 sent。
	seedOutbox(t, data, "ob-dead", OutboxDead)
	if transitioned, err := data.Outbox().MarkSent(ctx, "ob-dead"); err != nil {
		t.Fatalf("MarkSent on dead 行应静默无操作: %v", err)
	} else if transitioned {
		t.Fatal("MarkSent on dead 行不应报告状态已推进")
	}
	rows, _, err = data.Outbox().List(ctx, ListFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		if row.ID == "ob-dead" && row.Status != OutboxDead {
			t.Fatalf("dead 行被改成 %q", row.Status)
		}
	}
}

// TestMarkTransitionsStillWorkInFlight 对照：在途状态（pending/sending）的正常推进不受影响。
func TestMarkTransitionsStillWorkInFlight(t *testing.T) {
	data := openOutboxProbeStore(t)
	ctx := t.Context()

	seedOutbox(t, data, "ob-pending", OutboxPending)
	if transitioned, err := data.Outbox().MarkSent(ctx, "ob-pending"); err != nil {
		t.Fatalf("MarkSent: %v", err)
	} else if !transitioned {
		t.Fatal("在途行的 MarkSent 应报告状态已推进")
	}
	seedOutbox(t, data, "ob-sending", OutboxSending)
	if transitioned, err := data.Outbox().MarkDead(ctx, "ob-sending", "probe_dead"); err != nil {
		t.Fatalf("MarkDead: %v", err)
	} else if !transitioned {
		t.Fatal("在途行的 MarkDead 应报告状态已推进")
	}
	seedOutbox(t, data, "ob-retry", OutboxPending)
	if transitioned, err := data.Outbox().MarkRetry(ctx, "ob-retry", time.Now().UTC().Add(time.Minute), "probe_retry"); err != nil {
		t.Fatalf("MarkRetry: %v", err)
	} else if !transitioned {
		t.Fatal("在途行的 MarkRetry 应报告状态已推进")
	}

	rows, _, err := data.Outbox().List(ctx, ListFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]string{
		"ob-pending": OutboxSent,
		"ob-sending": OutboxDead,
		"ob-retry":   OutboxPending,
	}
	for _, row := range rows {
		if w, ok := want[row.ID]; ok && row.Status != w {
			t.Fatalf("%s 应推进到 %q，实际 %q", row.ID, w, row.Status)
		}
	}
}

// TestRetryDeadResetsAttemptCount 回归：人工从管理台触发 RetryDead 或 RetryAllDead 时，
// 必须重置 attempt_count 为 0。
// 否则死信行已积累 8 次重试上限，worker 领取后一旦遇到任意瞬时故障（如临时网络抖动、429 退避）
// 就会因 item.AttemptCount >= maxAttempts 直接再次打入死信，失去阶梯退避能力。
func TestRetryDeadResetsAttemptCount(t *testing.T) {
	data := openOutboxProbeStore(t)
	ctx := t.Context()

	// 1. 创建一条 attempt_count=8 的死信
	if _, err := data.Outbox().Create(ctx, NotificationOutbox{
		ID:             "ob-dead-max-attempts",
		ChannelID:      "ch-1",
		Status:         OutboxDead,
		Title:          "probe-dead",
		AttemptCount:   8,
		IdempotencyKey: "probe-dead-max-attempts",
	}); err != nil {
		t.Fatalf("seed dead outbox: %v", err)
	}

	// 2. 单条重试
	next := time.Now().UTC()
	if err := data.Outbox().RetryDead(ctx, "ob-dead-max-attempts", next); err != nil {
		t.Fatalf("RetryDead failed: %v", err)
	}

	rows, _, err := data.Outbox().List(ctx, ListFilter{PerPage: 10})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	var found *NotificationOutbox
	for _, r := range rows {
		if r.ID == "ob-dead-max-attempts" {
			found = &r
			break
		}
	}
	if found == nil {
		t.Fatal("retried item not found")
	}
	if found.Status != OutboxPending {
		t.Fatalf("expected status %s, got %s", OutboxPending, found.Status)
	}
	if found.AttemptCount != 0 {
		t.Fatalf("expected AttemptCount reset to 0, got %d", found.AttemptCount)
	}

	// 3. 测试 RetryAllDead 批量重置
	if _, err := data.Outbox().Create(ctx, NotificationOutbox{
		ID:             "ob-dead-batch-1",
		ChannelID:      "ch-2",
		Status:         OutboxDead,
		Title:          "probe-dead-2",
		AttemptCount:   8,
		IdempotencyKey: "probe-dead-batch-1",
	}); err != nil {
		t.Fatalf("seed dead outbox 2: %v", err)
	}

	retriedCount, err := data.Outbox().RetryAllDead(ctx, []string{"ch-2"}, next)
	if err != nil {
		t.Fatalf("RetryAllDead failed: %v", err)
	}
	if retriedCount != 1 {
		t.Fatalf("expected 1 retried item, got %d", retriedCount)
	}

	rows2, _, err := data.Outbox().List(ctx, ListFilter{PerPage: 10})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	for _, r := range rows2 {
		if r.ID == "ob-dead-batch-1" {
			if r.Status != OutboxPending {
				t.Fatalf("expected status %s, got %s", OutboxPending, r.Status)
			}
			if r.AttemptCount != 0 {
				t.Fatalf("expected AttemptCount reset to 0 in RetryAllDead, got %d", r.AttemptCount)
			}
		}
	}
}
