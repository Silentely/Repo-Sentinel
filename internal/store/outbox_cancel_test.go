package store

import (
	"context"
	"testing"
	"time"
)

// newCancelFixture 建一个可用渠道与基础时间，供取消用例复用。
func newCancelFixture(t *testing.T) (Store, context.Context) {
	t.Helper()
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.Channels().Upsert(ctx, NotificationChannel{
		ID:          "ch-cancel",
		ChannelType: ChannelTelegram,
		Name:        "cancel-channel",
		Target:      "123456",
		Enabled:     true,
		EventKinds:  []string{ReleaseKind},
	}); err != nil {
		t.Fatalf("upsert channel failed: %v", err)
	}
	return st, ctx
}

// createCancelRow 写入一条通知；kind/repository 落在 body_json（与 rules.Engine 实际写入一致）。
func createCancelRow(t *testing.T, st Store, ctx context.Context, id, repo, kind, status string) NotificationOutbox {
	t.Helper()
	row, err := st.Outbox().Create(ctx, NotificationOutbox{
		ID:             id,
		ChannelID:      "ch-cancel",
		IdempotencyKey: "idem-" + id,
		Status:         status,
		NextAttemptAt:  time.Now().UTC(),
		Title:          "release " + id,
		BodyText:       "body " + id,
		BodyJSON: map[string]any{
			"kind":       kind,
			"repository": repo,
			"event_id":   "ev-" + id,
		},
	})
	if err != nil {
		t.Fatalf("create outbox %s failed: %v", id, err)
	}
	return row
}

func outboxStatusByID(t *testing.T, st Store, ctx context.Context, id string) string {
	t.Helper()
	// 每页上限 100（NormalizeListFilter 钳制），积压用例需翻页才能覆盖目标行。
	for page := 1; page <= 10; page++ {
		items, _, err := st.Outbox().List(ctx, ListFilter{PerPage: 100, Page: page})
		if err != nil {
			t.Fatalf("list outbox failed: %v", err)
		}
		for _, item := range items {
			if item.ID == id {
				return item.Status
			}
		}
		if len(items) < 100 {
			break
		}
	}
	t.Fatalf("outbox %s 未找到", id)
	return ""
}

// TestCancelPendingByRepositoryScopedToTargetRelease 守护取消语义的边界：
// 只取消目标仓库的 pending Release 行——异仓库、非 Release 类别与 sending/dead/sent 行
// 一律保持原状。同步验证冗余列随写入派生（创建路径即冻结匹配依据）。
func TestCancelPendingByRepositoryScopedToTargetRelease(t *testing.T) {
	st, ctx := newCancelFixture(t)
	noiseProbeID := ""

	// 背景积压：异仓库 300 条待投递，验证取消不受积压量影响（旧实现需全量拉取 pending 行）。
	for i := 0; i < 300; i++ {
		id := "ob-noise-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		createCancelRow(t, st, ctx, id, "other/noise", ReleaseKind, OutboxPending)
		if i == 0 {
			noiseProbeID = id
		}
	}
	// 目标仓库：3 条待投递 + 各状态干扰行。
	for i := 0; i < 3; i++ {
		createCancelRow(t, st, ctx, "ob-target-"+string(rune('a'+i)), "target/repo", ReleaseKind, OutboxPending)
	}
	createCancelRow(t, st, ctx, "ob-target-sending", "target/repo", ReleaseKind, OutboxSending)
	createCancelRow(t, st, ctx, "ob-target-sent", "target/repo", ReleaseKind, OutboxSent)
	createCancelRow(t, st, ctx, "ob-target-dead", "target/repo", ReleaseKind, OutboxDead)
	// 非 Release 类别即使带同仓库名也不取消。
	createCancelRow(t, st, ctx, "ob-target-issue", "target/repo", WorkItemKindIssue, OutboxPending)

	n, err := st.Outbox().CancelPendingByRepository(ctx, "target/repo")
	if err != nil {
		t.Fatalf("CancelPendingByRepository failed: %v", err)
	}
	if n != 3 {
		t.Fatalf("应取消 3 条目标仓库待投递 Release 通知，实际 %d", n)
	}

	for i := 0; i < 3; i++ {
		id := "ob-target-" + string(rune('a'+i))
		if got := outboxStatusByID(t, st, ctx, id); got != OutboxCancelled {
			t.Fatalf("%s 应已取消，实际 %s", id, got)
		}
	}
	for _, id := range []string{"ob-target-sending", "ob-target-sent", "ob-target-dead", "ob-target-issue"} {
		if got := outboxStatusByID(t, st, ctx, id); got == OutboxCancelled {
			t.Fatalf("%s 不应被取消", id)
		}
	}
	if got := outboxStatusByID(t, st, ctx, noiseProbeID); got != OutboxPending {
		t.Fatalf("异仓库积压不应受影响，实际 %s", got)
	}

	// 取消结果幂等：再次调用不应影响任何行。
	again, err := st.Outbox().CancelPendingByRepository(ctx, "target/repo")
	if err != nil {
		t.Fatalf("二次取消失败: %v", err)
	}
	if again != 0 {
		t.Fatalf("二次取消应返回 0，实际 %d", again)
	}
}

// TestCancelPendingByRepositorySurvivesEventRemoval 守护取消不依赖关联事件：
// 事件被保留策略清理（或仓库级联删除）后，冗余列仍能独立定位该仓库的待投递通知。
// 旧实现逐行回查事件，事件缺失即静默漏取消。
func TestCancelPendingByRepositorySurvivesEventRemoval(t *testing.T) {
	st, ctx := newCancelFixture(t)

	event, err := st.Events().Create(ctx, Event{
		ID:             "ev-release-1",
		Kind:           ReleaseKind,
		Action:         "published",
		PayloadSummary: map[string]any{"repository": "target/repo", "tag": "v1.2.3"},
		OccurredAt:     time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create event failed: %v", err)
	}
	row, err := st.Outbox().Create(ctx, NotificationOutbox{
		ID:             "ob-legacy-shape",
		ChannelID:      "ch-cancel",
		EventID:        &event.ID,
		IdempotencyKey: "idem-legacy",
		Status:         OutboxPending,
		NextAttemptAt:  time.Now().UTC(),
		Title:          "release v1.2.3",
		BodyText:       "body",
		BodyJSON:       map[string]any{"kind": ReleaseKind, "repository": "target/repo"},
	})
	if err != nil {
		t.Fatalf("create outbox failed: %v", err)
	}
	if row.RepositoryFullName != "target/repo" {
		t.Fatalf("写入时应派生冗余仓库名，实际 %q", row.RepositoryFullName)
	}

	// 事件先于通知被清理：保留策略/级联删除都会造成这种时序。
	deleted, err := st.Events().DeleteOlderThan(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("delete events failed: %v", err)
	}
	if deleted < 1 {
		t.Fatal("测试事件应被清理")
	}

	n, err := st.Outbox().CancelPendingByRepository(ctx, "target/repo")
	if err != nil {
		t.Fatalf("CancelPendingByRepository failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("事件缺失后仍应取消 1 条，实际 %d", n)
	}
	if got := outboxStatusByID(t, st, ctx, "ob-legacy-shape"); got != OutboxCancelled {
		t.Fatalf("通知应已取消，实际 %s", got)
	}
}

// TestOutboxRepositoryFullNameOnlyForRelease 守护冗余列的填充范围：
// 非 Release 类别（含聚合摘要）留空，避免按仓库取消误伤其它类型通知。
func TestOutboxRepositoryFullNameOnlyForRelease(t *testing.T) {
	st, ctx := newCancelFixture(t)

	releaseRow := createCancelRow(t, st, ctx, "ob-rel", "target/repo", ReleaseKind, OutboxPending)
	if releaseRow.RepositoryFullName != "target/repo" {
		t.Fatalf("Release 通知应填充冗余仓库名，实际 %q", releaseRow.RepositoryFullName)
	}
	aggregateRow, err := st.Outbox().Create(ctx, NotificationOutbox{
		ID:             "ob-aggregate",
		ChannelID:      "ch-cancel",
		IdempotencyKey: "idem-aggregate",
		Status:         OutboxPending,
		NextAttemptAt:  time.Now().UTC(),
		Title:          "aggregated",
		BodyText:       "body",
		BodyJSON:       map[string]any{"aggregate": true, "count": 3},
	})
	if err != nil {
		t.Fatalf("create aggregate outbox failed: %v", err)
	}
	if aggregateRow.RepositoryFullName != "" {
		t.Fatalf("聚合摘要不应填充冗余仓库名，实际 %q", aggregateRow.RepositoryFullName)
	}

	n, err := st.Outbox().CancelPendingByRepository(ctx, "target/repo")
	if err != nil {
		t.Fatalf("CancelPendingByRepository failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("应只取消 Release 通知，实际 %d", n)
	}
	if got := outboxStatusByID(t, st, ctx, "ob-aggregate"); got != OutboxPending {
		t.Fatalf("聚合摘要不应被取消，实际 %s", got)
	}
}
