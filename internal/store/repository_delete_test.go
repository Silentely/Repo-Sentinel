package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestDeleteRepositoryCascade 守护仓库级联删除：
// work_items / workflow_runs / security_alerts / events（含引用这些事件的 outbox）/
// sync_cursors / repo_stat_snapshots 与仓库行本身全部删除，且不影响其他仓库的数据。
func TestDeleteRepositoryCascade(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	repo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-a", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	// 另一个仓库用于隔离断言：删除 repo-a 不得波及 repo-b。
	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-b", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "other", FullName: "acme/other",
		HTMLURL: "https://github.com/acme/other",
	}); err != nil {
		t.Fatalf("upsert repo-b: %v", err)
	}

	// 工作项（打开中的 PR/Issue 是用户明确要求的清理对象）。
	if _, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID: "wi-1", RepositoryID: repo.ID, Number: 7, Kind: store.WorkItemKindPR,
		State: "open", Title: "打开的 PR", Author: "alice", HTMLURL: "https://github.com/acme/demo/pull/7",
		SourceUpdatedAt: now, StateHash: "h1",
	}, nil); err != nil {
		t.Fatalf("upsert work item: %v", err)
	}
	if _, _, err := data.WorkflowRuns().UpsertIfNewer(ctx, store.WorkflowRun{
		ID: "wr-1", RepositoryID: repo.ID, GitHubRunID: 100, GitHubWorkflowID: 1,
		WorkflowName: "ci", RunNumber: 1, Event: "push", HeadBranch: "main", HeadSHA: "abc",
		Status: "completed", RunUpdatedAt: now, StateHash: "h2",
	}); err != nil {
		t.Fatalf("upsert workflow run: %v", err)
	}
	if _, _, err := data.SecurityAlerts().UpsertIfNewer(ctx, store.SecurityAlert{
		ID: "sa-1", RepositoryID: repo.ID, AlertKind: store.AlertKindDependabot, AlertNumber: 1,
		State: "open", Severity: "high", RuleOrDependency: "lodash", HTMLURL: "https://github.com/acme/demo/security",
		SourceUpdatedAt: now, StateHash: "h3",
	}); err != nil {
		t.Fatalf("upsert security alert: %v", err)
	}

	// 事件 + 引用该事件的 Outbox（单条与聚合投递都挂 event_id）。
	repoID := repo.ID
	ev, err := data.Events().Create(ctx, store.Event{
		ID: "ev-1", Source: "webhook", Kind: store.WorkItemKindPR, Action: "opened",
		RepositoryID: &repoID, Title: "打开的 PR", Actor: "alice", OccurredAt: now,
		HTMLURL: "https://github.com/acme/demo/pull/7", DedupeFingerprint: "fp-1", StateHash: "h4",
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-1", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if _, err := data.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "ob-1", ChannelID: ch.ID, EventID: &ev.ID, IdempotencyKey: "ob-1",
		Status: store.OutboxPending, Title: "通知", BodyText: "x", NextAttemptAt: now,
	}); err != nil {
		t.Fatalf("create outbox: %v", err)
	}

	if _, err := data.Cursors().Upsert(ctx, store.SyncCursor{
		RepositoryID: repo.ID, Resource: "issues", CursorValue: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("upsert cursor: %v", err)
	}
	if _, err := data.RepoStatSnapshots().Upsert(ctx, store.RepoStatSnapshot{
		RepositoryID: repo.ID, Metric: "stargazers", Value: 42, SampleDate: "2026-08-01",
	}); err != nil {
		t.Fatalf("upsert snapshot: %v", err)
	}

	// 执行级联删除。
	if err := data.Repositories().DeleteRepository(ctx, repo.ID); err != nil {
		t.Fatalf("delete repository: %v", err)
	}

	// 仓库行消失。
	if _, err := data.Repositories().Get(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("仓库应已删除，got err=%v", err)
	}
	// 关联数据全部清空。
	if _, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 7); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("work item 应已删除，got err=%v", err)
	}
	if _, err := data.WorkflowRuns().GetByRepoRunID(ctx, repo.ID, 100); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("workflow run 应已删除，got err=%v", err)
	}
	if _, err := data.SecurityAlerts().GetByIdentity(ctx, repo.ID, store.AlertKindDependabot, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("security alert 应已删除，got err=%v", err)
	}
	if _, err := data.Events().GetByFingerprint(ctx, "fp-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("event 应已删除，got err=%v", err)
	}
	if _, err := data.Cursors().Get(ctx, repo.ID, "issues"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cursor 应已删除，got err=%v", err)
	}
	if items, err := data.RepoStatSnapshots().ListInRange(ctx, []string{repo.ID}, "stargazers", "2026-01-01", "2026-12-31"); err != nil || len(items) != 0 {
		t.Fatalf("snapshot 应已删除，items=%v err=%v", items, err)
	}
	// 引用已删事件的 Outbox 一并清空。
	ob, page, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	for _, item := range ob {
		if item.ID == "ob-1" {
			t.Fatal("outbox 应已随事件删除")
		}
	}
	if page.Total != 0 {
		t.Fatalf("outbox 应全部删除，total=%d", page.Total)
	}

	// 隔离断言：repo-b 与其数据不受影响。
	keep, err := data.Repositories().Get(ctx, "repo-b")
	if err != nil || keep.FullName != "acme/other" {
		t.Fatalf("repo-b 不应被删除: %+v err=%v", keep, err)
	}
}

// TestDeleteRepositoryMissing 幂等语义：删除不存在的仓库返回 ErrNotFound，
// 调用方（webhook 处理器）可按幂等忽略，不产生副作用。
func TestDeleteRepositoryMissing(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	if err := data.Repositories().DeleteRepository(ctx, "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("删除不存在的仓库应返回 ErrNotFound，got %v", err)
	}
}
