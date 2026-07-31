package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + dbPath + "?_fk=1",
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestListExcludesArchivedReposAndIgnoredItems(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	active, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-active", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "active", FullName: "o/active", IsArchived: false,
		MonitorEnabled: true, IssuesEnabled: true, PrEnabled: true, ActionsEnabled: true, AlertsEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert active: %v", err)
	}
	archived, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-archived", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusArchived,
		Owner: "o", Name: "archived", FullName: "o/archived", IsArchived: true,
		MonitorEnabled: false, IssuesEnabled: false, PrEnabled: false, ActionsEnabled: false, AlertsEnabled: false,
	})
	if err != nil {
		t.Fatalf("upsert archived: %v", err)
	}

	for _, item := range []store.WorkItem{
		{ID: "wi-1", RepositoryID: active.ID, Number: 1, Kind: store.WorkItemKindIssue, State: "open", Title: "active open", SourceUpdatedAt: now, StateHash: "a1"},
		{ID: "wi-2", RepositoryID: active.ID, Number: 2, Kind: store.WorkItemKindIssue, State: "open", Title: "active ignored", SourceUpdatedAt: now, StateHash: "a2"},
		{ID: "wi-3", RepositoryID: archived.ID, Number: 3, Kind: store.WorkItemKindIssue, State: "open", Title: "archived open", SourceUpdatedAt: now, StateHash: "a3"},
		{ID: "wi-4", RepositoryID: active.ID, Number: 4, Kind: store.WorkItemKindPR, State: "open", Title: "active pr", SourceUpdatedAt: now, StateHash: "a4"},
	} {
		if _, _, err := data.WorkItems().UpsertIfNewer(ctx, item); err != nil {
			t.Fatalf("upsert work item %s: %v", item.ID, err)
		}
	}
	if err := data.WorkItems().SetIgnored(ctx, "wi-2", true); err != nil {
		t.Fatalf("ignore wi-2: %v", err)
	}

	items, page, err := data.WorkItems().List(ctx, store.ListFilter{Page: 1, PerPage: 50, Kind: store.WorkItemKindIssue, State: "open"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(items) != 1 || items[0].ID != "wi-1" {
		t.Fatalf("默认列表应只含未归档且未忽略的 issue，got total=%d items=%v", page.Total, items)
	}

	ignored, page, err := data.WorkItems().List(ctx, store.ListFilter{Page: 1, PerPage: 50, Kind: store.WorkItemKindIssue, OnlyIgnored: true})
	if err != nil {
		t.Fatalf("list ignored: %v", err)
	}
	if page.Total != 1 || len(ignored) != 1 || ignored[0].ID != "wi-2" {
		t.Fatalf("OnlyIgnored 应只返回 wi-2，got total=%d items=%v", page.Total, ignored)
	}

	openIssues, err := data.WorkItems().CountOpen(ctx)
	if err != nil {
		t.Fatalf("CountOpen: %v", err)
	}
	if openIssues != 1 {
		t.Fatalf("CountOpen 应排除 PR/归档/忽略，got %d", openIssues)
	}

	stats, err := data.Dashboard(ctx)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if stats.OpenIssues != 1 {
		t.Fatalf("dashboard open_issues want 1 got %d", stats.OpenIssues)
	}
	if stats.OpenPulls != 1 {
		t.Fatalf("dashboard open_pulls want 1 got %d", stats.OpenPulls)
	}
}

func TestWorkflowRunListRespectsArchivedAndIgnored(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	active, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-a", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "a", FullName: "o/a",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	archived, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-b", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusArchived,
		Owner: "o", Name: "b", FullName: "o/b", IsArchived: true,
	})
	if err != nil {
		t.Fatalf("upsert archived: %v", err)
	}

	fail := "failure"
	for _, run := range []store.WorkflowRun{
		{ID: "run-1", RepositoryID: active.ID, GitHubRunID: 1, WorkflowName: "ci", Status: "completed", Conclusion: &fail, RunUpdatedAt: now, StateHash: "r1"},
		{ID: "run-2", RepositoryID: active.ID, GitHubRunID: 2, WorkflowName: "ci", Status: "completed", Conclusion: &fail, RunUpdatedAt: now, StateHash: "r2"},
		{ID: "run-3", RepositoryID: archived.ID, GitHubRunID: 3, WorkflowName: "ci", Status: "completed", Conclusion: &fail, RunUpdatedAt: now, StateHash: "r3"},
	} {
		if _, _, err := data.WorkflowRuns().UpsertIfNewer(ctx, run); err != nil {
			t.Fatalf("upsert run: %v", err)
		}
	}
	if err := data.WorkflowRuns().SetIgnored(ctx, "run-2", true); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	items, page, err := data.WorkflowRuns().List(ctx, store.ListFilter{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(items) != 1 || items[0].ID != "run-1" {
		t.Fatalf("默认应只返回 run-1，got total=%d items=%v", page.Total, items)
	}
	failed, err := data.WorkflowRuns().CountFailed(ctx)
	if err != nil {
		t.Fatalf("CountFailed: %v", err)
	}
	if failed != 1 {
		t.Fatalf("CountFailed want 1 got %d", failed)
	}
}

func TestSecurityAlertListRespectsArchivedAndIgnored(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	active, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-sa", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "sa", FullName: "o/sa",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	archived, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-sb", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusArchived,
		Owner: "o", Name: "sb", FullName: "o/sb", IsArchived: true,
	})
	if err != nil {
		t.Fatalf("upsert archived: %v", err)
	}

	for _, a := range []store.SecurityAlert{
		{ID: "sa-1", RepositoryID: active.ID, AlertKind: store.AlertKindDependabot, AlertNumber: 1, State: "open", Severity: "high", SourceUpdatedAt: now, StateHash: "s1"},
		{ID: "sa-2", RepositoryID: active.ID, AlertKind: store.AlertKindDependabot, AlertNumber: 2, State: "open", Severity: "low", SourceUpdatedAt: now, StateHash: "s2"},
		{ID: "sa-3", RepositoryID: archived.ID, AlertKind: store.AlertKindDependabot, AlertNumber: 3, State: "open", Severity: "high", SourceUpdatedAt: now, StateHash: "s3"},
	} {
		if _, _, err := data.SecurityAlerts().UpsertIfNewer(ctx, a); err != nil {
			t.Fatalf("upsert alert: %v", err)
		}
	}
	if err := data.SecurityAlerts().SetIgnored(ctx, "sa-2", true); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	items, page, err := data.SecurityAlerts().List(ctx, store.ListFilter{Page: 1, PerPage: 50, State: "open"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 || len(items) != 1 || items[0].ID != "sa-1" {
		t.Fatalf("默认应只返回 sa-1，got total=%d items=%v", page.Total, items)
	}
	open, err := data.SecurityAlerts().CountOpen(ctx)
	if err != nil {
		t.Fatalf("CountOpen: %v", err)
	}
	if open != 1 {
		t.Fatalf("CountOpen want 1 got %d", open)
	}

	// 指定归档仓 ID 时应仍可按 repository_id 查看（不强制排除）。
	byArchived, page, err := data.SecurityAlerts().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, RepositoryID: archived.ID, State: "open",
	})
	if err != nil {
		t.Fatalf("list by archived: %v", err)
	}
	if page.Total != 1 || len(byArchived) != 1 || byArchived[0].ID != "sa-3" {
		t.Fatalf("按归档仓 ID 筛选应返回 sa-3，got %+v", byArchived)
	}
}

func TestListIncludeArchivedReposOptIn(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	active, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ia", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "ia", FullName: "o/ia",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ib", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusArchived,
		Owner: "o", Name: "ib", FullName: "o/ib", IsArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []store.WorkItem{
		{ID: "wia-1", RepositoryID: active.ID, Number: 1, Kind: store.WorkItemKindIssue, State: "open", Title: "a", SourceUpdatedAt: now, StateHash: "x1"},
		{ID: "wia-2", RepositoryID: archived.ID, Number: 2, Kind: store.WorkItemKindIssue, State: "open", Title: "b", SourceUpdatedAt: now, StateHash: "x2"},
	} {
		if _, _, err := data.WorkItems().UpsertIfNewer(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	all, page, err := data.WorkItems().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, Kind: store.WorkItemKindIssue, IncludeArchivedRepos: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(all) != 2 {
		t.Fatalf("IncludeArchivedRepos 应返回 2 条，got total=%d len=%d", page.Total, len(all))
	}
}

func TestOutboxListFiltersByChannelIDs(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	// 渠道类型过滤必须下沉到 SQL：分页与 total 才不会被内存过滤改写失真。
	for _, item := range []store.NotificationOutbox{
		{ID: "ob-1", ChannelID: "ch-tg-1", IdempotencyKey: "k|1", Status: store.OutboxPending, NextAttemptAt: now, Title: "a"},
		{ID: "ob-2", ChannelID: "ch-tg-2", IdempotencyKey: "k|2", Status: store.OutboxPending, NextAttemptAt: now, Title: "b"},
		{ID: "ob-3", ChannelID: "ch-wh-1", IdempotencyKey: "k|3", Status: store.OutboxPending, NextAttemptAt: now, Title: "c"},
	} {
		if _, err := data.Outbox().Create(ctx, item); err != nil {
			t.Fatalf("create %s: %v", item.ID, err)
		}
	}

	items, page, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 1, ChannelIDs: []string{"ch-tg-1", "ch-tg-2"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 2 || len(items) != 1 {
		t.Fatalf("渠道集合过滤 total 应为 2 且每页 1 条，got total=%d len=%d", page.Total, len(items))
	}
	if items[0].ChannelID == "ch-wh-1" {
		t.Fatalf("不应返回渠道集合之外的条目: %+v", items[0])
	}

	empty, page, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 50, ChannelIDs: []string{"ch-tg-1"}})
	if err != nil {
		t.Fatalf("list single: %v", err)
	}
	if page.Total != 1 || len(empty) != 1 || empty[0].ID != "ob-1" {
		t.Fatalf("单渠道过滤应只返回 ob-1，got total=%d items=%v", page.Total, empty)
	}
}

func TestWorkItemListFiltersPRReviewAndCheck(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	repo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-pr", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "pr", FullName: "o/pr",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []store.WorkItem{
		{ID: "pr-1", RepositoryID: repo.ID, Number: 1, Kind: store.WorkItemKindPR, State: "open", Title: "approved", SourceUpdatedAt: now, StateHash: "p1", ReviewDecision: "approved", CheckStatus: "success"},
		{ID: "pr-2", RepositoryID: repo.ID, Number: 2, Kind: store.WorkItemKindPR, State: "open", Title: "changes", SourceUpdatedAt: now, StateHash: "p2", ReviewDecision: "changes_requested", CheckStatus: "failure"},
		{ID: "pr-3", RepositoryID: repo.ID, Number: 3, Kind: store.WorkItemKindPR, State: "open", Title: "reviewing", SourceUpdatedAt: now, StateHash: "p3", ReviewState: "PENDING", CheckStatus: "pending"},
		{ID: "pr-4", RepositoryID: repo.ID, Number: 4, Kind: store.WorkItemKindPR, State: "open", Title: "no checks", SourceUpdatedAt: now, StateHash: "p4"},
	} {
		if _, _, err := data.WorkItems().UpsertIfNewer(ctx, item); err != nil {
			t.Fatalf("upsert %s: %v", item.ID, err)
		}
	}

	approved, page, err := data.WorkItems().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, Kind: store.WorkItemKindPR, ReviewDecision: "approved",
	})
	if err != nil {
		t.Fatalf("list approved: %v", err)
	}
	if page.Total != 1 || len(approved) != 1 || approved[0].ID != "pr-1" {
		t.Fatalf("review=approved 应只返回 pr-1，got total=%d items=%v", page.Total, approved)
	}

	failed, page, err := data.WorkItems().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, Kind: store.WorkItemKindPR, CheckStatus: "failure",
	})
	if err != nil {
		t.Fatalf("list check failure: %v", err)
	}
	if page.Total != 1 || len(failed) != 1 || failed[0].ID != "pr-2" {
		t.Fatalf("check=failed 应只返回 pr-2，got total=%d items=%v", page.Total, failed)
	}

	// pending 覆盖「无检查数据（空串）」与「检查进行中（pending）」两种记录。
	pending, page, err := data.WorkItems().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, Kind: store.WorkItemKindPR, CheckStatus: "pending",
	})
	if err != nil {
		t.Fatalf("list check pending: %v", err)
	}
	if page.Total != 2 || len(pending) != 2 {
		t.Fatalf("check=pending 应返回 pr-3 与 pr-4，got total=%d items=%v", page.Total, pending)
	}

	// 「审核中」= 尚无审核结论（pr-3 审核中、pr-4 无任何审核数据）。
	reviewPending, page, err := data.WorkItems().List(ctx, store.ListFilter{
		Page: 1, PerPage: 50, Kind: store.WorkItemKindPR, ReviewDecision: "pending",
	})
	if err != nil {
		t.Fatalf("list review pending: %v", err)
	}
	if page.Total != 2 || len(reviewPending) != 2 {
		t.Fatalf("review=pending 应返回 pr-3 与 pr-4，got total=%d items=%v", page.Total, reviewPending)
	}
}

func TestListSyncCandidatesOrdersByLastSyncedAt(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)

	older := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	mk := func(id string, synced *time.Time) {
		t.Helper()
		if _, err := data.Repositories().Upsert(ctx, store.Repository{
			ID: id, Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
			Owner: "o", Name: id, FullName: "o/" + id, LastSyncedAt: synced,
		}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}
	mk("repo-recent", &recent)
	mk("repo-never", nil) // 从未同步：必须排最前
	mk("repo-older", &older)
	// 外部仓不应混入 installation 候选。
	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ext", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "ext", FullName: "o/ext",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := data.Repositories().ListSyncCandidates(ctx, store.RepositoryTypeInstallation, 10)
	if err != nil {
		t.Fatalf("ListSyncCandidates: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应只有 3 个 installation 仓，got %d", len(got))
	}
	want := []string{"repo-never", "repo-older", "repo-recent"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("候选顺序应为 %v，got [%s %s %s]", want, got[0].ID, got[1].ID, got[2].ID)
		}
	}
}
