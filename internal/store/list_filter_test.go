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
