package normalizer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestProcessIssueOpenedCreatesEvent(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "n.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":     7,
			"title":      "hello",
			"state":      "open",
			"html_url":   "https://github.com/acme/demo/issues/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": map[string]any{
			"id":             99,
			"name":           "demo",
			"full_name":      "acme/demo",
			"private":        false,
			"html_url":       "https://github.com/acme/demo",
			"default_branch": "main",
			"owner":          map[string]any{"login": "acme"},
		},
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "issues", "delivery-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil {
		t.Fatal("期望产生事件")
	}
	if res.Repository == nil || res.Repository.FullName != "acme/demo" {
		t.Fatalf("仓库不正确: %+v", res.Repository)
	}
	// 基线中应抑制通知
	if !res.SuppressNotify {
		t.Fatal("新仓库应处于基线抑制")
	}
}

// demoRepoPayload 构造 acme/demo 的 repository 载荷；archived 控制 GitHub 侧归档标记。
func demoRepoPayload(archived bool) map[string]any {
	return map[string]any{
		"id":             99,
		"name":           "demo",
		"full_name":      "acme/demo",
		"private":        false,
		"archived":       archived,
		"html_url":       "https://github.com/acme/demo",
		"default_branch": "main",
		"owner":          map[string]any{"login": "acme"},
	}
}

// seedActiveDemoRepo 预置 acme/demo 为 active 状态（能力开关取 schema 默认值 true）。
func seedActiveDemoRepo(t *testing.T, data store.Store) store.Repository {
	t.Helper()
	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: "repo-demo", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	return repo
}

func openProcessStore(t *testing.T) store.Store {
	t.Helper()
	dbURL := "file:" + filepath.Join(t.TempDir(), "n.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

// 关闭 Actions 开关的仓库收到 workflow_run：不写 WorkflowRun、不建事件。
func TestProcessWorkflowRunRespectsActionsToggle(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)
	off := false
	if err := data.Repositories().UpdateSettings(t.Context(), repo.ID, store.RepositorySettings{ActionsEnabled: &off}); err != nil {
		t.Fatal(err)
	}

	conclusion := "failure"
	payload, _ := json.Marshal(map[string]any{
		"action": "completed",
		"workflow_run": map[string]any{
			"id": 12345, "name": "ci", "workflow_id": 77, "run_number": 9,
			"event": "push", "status": "completed", "conclusion": conclusion,
			"html_url":    "https://github.com/acme/demo/actions/runs/12345",
			"head_branch": "main", "head_sha": "abc1234", "run_attempt": 1,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"actor":      map[string]any{"login": "alice"},
		},
		"repository": demoRepoPayload(false),
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "workflow_run", "delivery-actions-off", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event != nil {
		t.Fatalf("Actions 关闭不应产生事件: %+v", res.Event)
	}
	if !res.SuppressNotify {
		t.Fatal("关闭的开关必须抑制通知")
	}
	_, page, err := data.WorkflowRuns().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("Actions 关闭不应落库 WorkflowRun，got total=%d", page.Total)
	}
}

// 全局 feature.actions 关闭时：即使仓库级 Actions 仍开启，也不落库、不建事件。
func TestProcessWorkflowRunRespectsGlobalFeatureActions(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)
	raw, _ := json.Marshal(false)
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: "set-actions-off", Key: store.SettingFeatureActions, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"action": "completed",
		"workflow_run": map[string]any{
			"id": 99901, "name": "质量守卫", "workflow_id": 88, "run_number": 3,
			"event": "push", "status": "completed", "conclusion": "cancelled",
			"html_url":    "https://github.com/acme/demo/actions/runs/99901",
			"head_branch": "main", "head_sha": "def5678", "run_attempt": 1,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"actor":      map[string]any{"login": "alice"},
		},
		"repository": demoRepoPayload(false),
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "workflow_run", "delivery-global-actions-off", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event != nil {
		t.Fatalf("全局 Actions 关闭不应产生事件: %+v", res.Event)
	}
	if !res.SuppressNotify {
		t.Fatal("全局关闭必须抑制通知")
	}
	// 仓库级开关仍为 true，证明拦截来自全局而非仓级。
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ActionsEnabled {
		t.Fatal("本用例要求仓库级 Actions 仍开启")
	}
	_, page, err := data.WorkflowRuns().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("全局 Actions 关闭不应落库 WorkflowRun，got total=%d", page.Total)
	}
}

// TestProcessIngestSkipLogsReason 采集跳过是"收到事件但没写数据"的静默路径：
// 注入 Logger 后必须输出 Debug 留痕并带具体原因（feature_disabled/capability_off 等），
// 否则用户开关与仓库状态变化无法从日志反推。
func TestProcessIngestSkipLogsReason(t *testing.T) {
	data := openProcessStore(t)
	// 预置仓库使载荷能解析到 acme/demo（全局开关关闭才是跳过原因）。
	seedActiveDemoRepo(t, data)
	raw, _ := json.Marshal(false)
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: "set-issues-off", Key: store.SettingFeatureIssues, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	var logBuffer bytes.Buffer
	proc := &normalizer.Processor{
		Store:  data,
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": 3, "title": "hello", "state": "open",
			"html_url":   "https://github.com/acme/demo/issues/3",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{}, "assignees": []any{},
		},
		"repository": demoRepoPayload(false),
	})
	if _, err := proc.Process(t.Context(), "issues", "delivery-skip-log", payload); err != nil {
		t.Fatal(err)
	}

	logs := logBuffer.String()
	for _, want := range []string{`"msg":"webhook ingest skipped"`, `"repo":"acme/demo"`, `"kind":"issue"`, `"reason":"feature_disabled"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("跳过留痕应包含 %s，实际: %s", want, logs)
		}
	}
}

// 已归档仓库的 PR 事件：不更新数据、不建事件。
func TestProcessSkipsArchivedRepo(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)
	archived := true
	if err := data.Repositories().UpdateSettings(t.Context(), repo.ID, store.RepositorySettings{IsArchived: &archived}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 11, "title": "archived pr", "state": "open",
			"html_url":   "https://github.com/acme/demo/pull/11",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{}, "assignees": []any{},
		},
		"repository": demoRepoPayload(false),
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "pull_request", "delivery-archived-pr", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event != nil {
		t.Fatalf("已归档仓库不应产生事件: %+v", res.Event)
	}
	if _, err := data.WorkItems().GetByRepoNumber(t.Context(), repo.ID, 11); err == nil {
		t.Fatal("已归档仓库不应落库 WorkItem")
	}
}

// GitHub 侧归档（payload.repository.archived=true）必须联动收口：
// sync_status=archived、开关全关，且该事件自身不再被采集。
func TestProcessLinksGitHubSideArchive(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)

	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": 21, "title": "late issue", "state": "open",
			"html_url":   "https://github.com/acme/demo/issues/21",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{}, "assignees": []any{},
		},
		"repository": demoRepoPayload(true),
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "issues", "delivery-gh-archive", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event != nil {
		t.Fatalf("GitHub 已归档仓库不应产生事件: %+v", res.Event)
	}
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusArchived || !got.IsArchived {
		t.Fatalf("归档联动失败: status=%s archived=%v", got.SyncStatus, got.IsArchived)
	}
	if got.MonitorEnabled || got.IssuesEnabled || got.PrEnabled || got.ActionsEnabled || got.AlertsEnabled {
		t.Fatalf("归档应联动关闭全部开关: %+v", got)
	}
}

// repository archived/unarchived 事件：归档与取消归档都必须联动开关。
func TestProcessRepositoryEventArchiveLinkage(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)
	proc := &normalizer.Processor{Store: data}

	archivedPayload, _ := json.Marshal(map[string]any{
		"action":     "archived",
		"repository": demoRepoPayload(true),
	})
	if _, err := proc.Process(t.Context(), "repository", "delivery-arch-1", archivedPayload); err != nil {
		t.Fatal(err)
	}
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusArchived || !got.IsArchived || got.MonitorEnabled {
		t.Fatalf("archived 事件应联动归档并关监控: %+v", got)
	}

	unarchivedPayload, _ := json.Marshal(map[string]any{
		"action":     "unarchived",
		"repository": demoRepoPayload(false),
	})
	if _, err := proc.Process(t.Context(), "repository", "delivery-arch-2", unarchivedPayload); err != nil {
		t.Fatal(err)
	}
	got, err = data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusActive || got.IsArchived || !got.MonitorEnabled {
		t.Fatalf("unarchived 事件应恢复监控: %+v", got)
	}
}

// 合并事件必须持久化 Merged：StateHash 不含 merged，若沿用 UpsertIfNewer 会恒早退。
func TestProcessPullRequestMergedPersistsFlag(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "n.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	repo := map[string]any{
		"id":             99,
		"name":           "demo",
		"full_name":      "acme/demo",
		"private":        false,
		"html_url":       "https://github.com/acme/demo",
		"default_branch": "main",
		"owner":          map[string]any{"login": "acme"},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	payload, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     7,
			"title":      "feat",
			"state":      "closed",
			"merged":     true,
			"html_url":   "https://github.com/acme/demo/pull/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": now,
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": repo,
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "pull_request", "delivery-m1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil {
		t.Fatal("期望产生事件")
	}
	item, err := data.WorkItems().GetByRepoNumber(t.Context(), res.Repository.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Merged {
		t.Fatal("合并事件应持久化 Merged=true")
	}

	// 后续未合并语义的更新不得回退 merged。
	payload2, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     7,
			"title":      "feat v2",
			"state":      "closed",
			"merged":     false,
			"html_url":   "https://github.com/acme/demo/pull/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": repo,
	})
	if _, err := proc.Process(t.Context(), "pull_request", "delivery-m2", payload2); err != nil {
		t.Fatal(err)
	}
	item, err = data.WorkItems().GetByRepoNumber(t.Context(), res.Repository.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Merged {
		t.Fatal("后续事件不应回退已合并标记")
	}
}

func TestProcessInstallationCreatedImportsTopLevelRepositories(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "install.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	// 对齐 GitHub installation.created 真实载荷：仓库在 top-level repositories，
	// 条目常只有 id/name/full_name/private（无 html_url、无 owner 对象）。
	payload, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id": 149631800,
			"account": map[string]any{
				"login": "Silentely",
				"type":  "User",
			},
		},
		"repositories": []any{
			map[string]any{
				"id":        1312928715,
				"name":      "Repo-Sentinel",
				"full_name": "Silentely/Repo-Sentinel",
				"private":   false,
			},
			map[string]any{
				"id":        239923774,
				"name":      "Demo",
				"full_name": "Silentely/Demo",
				"private":   false,
			},
		},
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "installation", "delivery-install-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated {
		t.Fatal("期望 Updated")
	}
	if !res.SuppressNotify {
		t.Fatal("安装导入应抑制通知洪流")
	}

	insts, err := data.Installations().List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 1 || insts[0].InstallationID != 149631800 {
		t.Fatalf("installation 未写入: %+v", insts)
	}
	if insts[0].AccountLogin != "Silentely" {
		t.Fatalf("account=%q", insts[0].AccountLogin)
	}

	page, _, err := data.Repositories().List(t.Context(), store.ListFilter{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("期望导入 2 个仓库，实际 %d: %+v", len(page), page)
	}
	names := map[string]string{}
	for _, r := range page {
		names[r.FullName] = r.SyncStatus
		if r.HTMLURL == "" {
			t.Fatalf("html_url 应被补全: %+v", r)
		}
		if r.SyncStatus != store.SyncStatusBaseline {
			t.Fatalf("新仓应为 baseline_sync: %+v", r)
		}
		if r.Owner == "" || r.Name == "" {
			t.Fatalf("owner/name 应从 full_name 解析: %+v", r)
		}
	}
	if _, ok := names["Silentely/Repo-Sentinel"]; !ok {
		t.Fatal("缺少 Silentely/Repo-Sentinel")
	}
	if _, ok := names["Silentely/Demo"]; !ok {
		t.Fatal("缺少 Silentely/Demo")
	}
}

func TestProcessInstallationRepositoriesAdded(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "install-added.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	payload, _ := json.Marshal(map[string]any{
		"action": "added",
		"installation": map[string]any{
			"id": 42,
			"account": map[string]any{
				"login": "acme",
				"type":  "Organization",
			},
		},
		"repositories_added": []any{
			map[string]any{
				"id":        1,
				"name":      "one",
				"full_name": "acme/one",
				"private":   true,
			},
		},
	})

	proc := &normalizer.Processor{Store: data}
	if _, err := proc.Process(t.Context(), "installation_repositories", "delivery-added-1", payload); err != nil {
		t.Fatal(err)
	}
	page, _, err := data.Repositories().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].FullName != "acme/one" || !page[0].IsPrivate {
		t.Fatalf("repositories_added 未导入: %+v", page)
	}
}

// TestProcessWorkflowRunWithMissingFields 验证 GitHub 偶发缺字段时 workflow_run 仍能安全入库。
func TestProcessWorkflowRunWithMissingFields(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "wf.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	// 模拟 GitHub 偶发缺 actor、head_branch、head_sha、html_url 等字段。
	payload, _ := json.Marshal(map[string]any{
		"workflow_run": map[string]any{
			"id":             999001,
			"name":           "",
			"workflow_id":    10,
			"run_number":     5,
			"event":          "push",
			"status":         "completed",
			"conclusion":     "success",
			"run_attempt":    0,
			"head_branch":    "",
			"head_sha":       "",
			"html_url":       "",
			"run_started_at": time.Now().UTC().Format(time.RFC3339),
			"updated_at":     time.Now().UTC().Format(time.RFC3339),
		},
		"repository": map[string]any{
			"id":             200,
			"name":           "test-repo",
			"full_name":      "acme/test-repo",
			"private":        false,
			"html_url":       "https://github.com/acme/test-repo",
			"default_branch": "main",
			"owner":          map[string]any{"login": "acme"},
		},
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "workflow_run", "delivery-wf-1", payload)
	if err != nil {
		t.Fatalf("缺字段 workflow_run 应安全处理，但报错: %v", err)
	}
	if res.Repository == nil {
		t.Fatal("期望返回仓库")
	}

	// 验证入库的 workflow_run 使用了默认值。
	run, err := data.WorkflowRuns().GetByRepoRunID(t.Context(), res.Repository.ID, 999001)
	if err != nil {
		t.Fatalf("workflow_run 未入库: %v", err)
	}
	if run.Actor == "" {
		t.Fatal("actor 应有默认值")
	}
	if run.HeadBranch == "" {
		t.Fatal("head_branch 应有默认值")
	}
	if run.HeadSHA == "" {
		t.Fatal("head_sha 应有默认值")
	}
	if run.HTMLURL == "" {
		t.Fatal("html_url 应有默认值")
	}
	if run.RunAttempt < 1 {
		t.Fatalf("run_attempt 应 >=1，实际 %d", run.RunAttempt)
	}
	if run.WorkflowName == "" {
		t.Fatal("workflow_name 应有默认值")
	}
}

// 上一次 completed 运行为失败、本次成功：应触发恢复事件。
// 回归：LatestCompleted 查询曾位于 UpsertIfNewer 之后，命中刚写入的当前运行，
// 导致 prevRun.GitHubRunID != run.ID 恒为 false，恢复事件永不触发。
func TestProcessWorkflowRunRecoveryDetected(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)

	// 预置上一次失败的 completed 运行（时间早于本次 webhook）。
	failure := "failure"
	prevTime := time.Now().UTC().Add(-time.Hour)
	prevCompleted := prevTime.Add(-time.Minute)
	if _, _, err := data.WorkflowRuns().UpsertIfNewer(t.Context(), store.WorkflowRun{
		RepositoryID:     repo.ID,
		GitHubRunID:      1001,
		GitHubWorkflowID: 77,
		WorkflowName:     "ci",
		RunNumber:        8,
		Event:            "push",
		HeadBranch:       "main",
		HeadSHA:          "abc1111",
		Status:           "completed",
		Conclusion:       &failure,
		Actor:            "alice",
		RunAttempt:       1,
		HTMLURL:          "https://github.com/acme/demo/actions/runs/1001",
		RunStartedAt:     &prevTime,
		RunUpdatedAt:     prevTime,
		RunCompletedAt:   &prevCompleted,
		StateHash:        "seed-state-1001",
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"action": "completed",
		"workflow_run": map[string]any{
			"id": 1002, "name": "ci", "workflow_id": 77, "run_number": 9,
			"event": "push", "status": "completed", "conclusion": "success",
			"html_url":    "https://github.com/acme/demo/actions/runs/1002",
			"head_branch": "main", "head_sha": "abc2222", "run_attempt": 1,
			"run_started_at": prevTime.Add(30 * time.Minute).Format(time.RFC3339),
			"updated_at":     time.Now().UTC().Format(time.RFC3339),
			"actor":          map[string]any{"login": "alice"},
		},
		"repository": demoRepoPayload(false),
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "workflow_run", "delivery-recovery-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil {
		t.Fatal("期望产生事件")
	}
	if res.Event.Action != "recovered" {
		t.Fatalf("期望 Action=recovered，实际 %q", res.Event.Action)
	}
	if got, ok := res.Event.PayloadSummary["previous_conclusion"]; !ok || got != "failure" {
		t.Fatalf("期望 previous_conclusion=failure，实际 %v", res.Event.PayloadSummary["previous_conclusion"])
	}
}

// TestUpsertPreservesCapabilitySettings 验证 Upsert 不会覆盖用户配置的能力开关。
func TestUpsertPreservesCapabilitySettings(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "cap.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	// 创建仓库（能力开关由 DB 默认值 true）。
	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID:       "repo-1",
		Type:     store.RepositoryTypeInstallation,
		Owner:    "acme",
		Name:     "demo",
		FullName: "acme/demo",
		HTMLURL:  "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.MonitorEnabled {
		t.Fatal("新仓库 monitor_enabled 应默认 true")
	}

	// 用户关闭 Issues 开关。
	falseVal := false
	if err := data.Repositories().UpdateSettings(t.Context(), repo.ID, store.RepositorySettings{
		IssuesEnabled: &falseVal,
	}); err != nil {
		t.Fatal(err)
	}

	// Upsert 同步元数据不应覆盖能力开关。
	_, err = data.Repositories().Upsert(t.Context(), store.Repository{
		Type:      store.RepositoryTypeInstallation,
		Owner:     "acme",
		Name:      "demo",
		FullName:  "acme/demo",
		HTMLURL:   "https://github.com/acme/demo",
		IsPrivate: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	saved, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.IssuesEnabled {
		t.Fatal("Upsert 不应覆盖用户关闭的 issues_enabled")
	}
	if !saved.MonitorEnabled {
		t.Fatal("Upsert 不应覆盖 monitor_enabled")
	}
	if !saved.IsPrivate {
		t.Fatal("Upsert 应更新 is_private 元数据")
	}
}

// TestUpdateSettingsAndArchive 验证 UpdateSettings 的归档联动。
func TestUpdateSettingsAndArchive(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "arch.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID:       "repo-2",
		Type:     store.RepositoryTypeInstallation,
		Owner:    "acme",
		Name:     "demo",
		FullName: "acme/demo",
		HTMLURL:  "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 归档仓库。
	archiveVal := true
	if err := data.Repositories().UpdateSettings(t.Context(), repo.ID, store.RepositorySettings{
		IsArchived: &archiveVal,
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsArchived {
		t.Fatal("归档应设置 is_archived=true")
	}
	if saved.SyncStatus != store.SyncStatusArchived {
		t.Fatalf("归档应联动 sync_status=archived，实际 %q", saved.SyncStatus)
	}
	// 归档时应联动关闭所有能力开关。
	if saved.MonitorEnabled || saved.IssuesEnabled || saved.PrEnabled || saved.ActionsEnabled || saved.AlertsEnabled {
		t.Fatal("归档应联动关闭所有能力开关")
	}

	// 取消归档。
	unarchiveVal := false
	if err := data.Repositories().UpdateSettings(t.Context(), repo.ID, store.RepositorySettings{
		IsArchived: &unarchiveVal,
	}); err != nil {
		t.Fatal(err)
	}
	saved, err = data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.IsArchived {
		t.Fatal("取消归档应设置 is_archived=false")
	}
	if saved.SyncStatus != store.SyncStatusActive {
		t.Fatalf("取消归档应联动 sync_status=active，实际 %q", saved.SyncStatus)
	}
	// 取消归档时应恢复所有能力开关。
	if !saved.MonitorEnabled || !saved.IssuesEnabled || !saved.PrEnabled || !saved.ActionsEnabled || !saved.AlertsEnabled {
		t.Fatal("取消归档应恢复所有能力开关为 true")
	}
}

// --- star / watch ---

// starPayload 构造 GitHub star 事件载荷（含 stargazers_count 实时值）。
func starPayload(action, login, starredAt string, stargazers int64) []byte {
	return []byte(fmt.Sprintf(`{
		"action": %q,
		"starred_at": %q,
		"sender": {"login": %q},
		"repository": {"id": 1001, "name": "r", "full_name": "o/r", "html_url": "https://github.com/o/r", "stargazers_count": %d, "owner": {"login": "o"}}
	}`, action, starredAt, login, stargazers))
}

// watchPayload 构造 GitHub watch 事件载荷。
func watchPayload(action, login string) []byte {
	return []byte(fmt.Sprintf(`{
		"action": %q,
		"sender": {"login": %q},
		"repository": {"id": 1001, "name": "r", "full_name": "o/r", "html_url": "https://github.com/o/r", "owner": {"login": "o"}}
	}`, action, login))
}

func newID() string { return ulid.Make().String() }

func TestProcessStarCreated(t *testing.T) {
	st := openProcessStore(t)
	p := &normalizer.Processor{Store: st}
	res, err := p.Process(context.Background(), "star", "dlv-1", starPayload("created", "alice", "2026-08-01T10:00:00Z", 42))
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil || res.Event.Kind != store.StarKind || res.Event.Action != "created" || res.Event.Actor != "alice" {
		t.Fatalf("unexpected event: %+v", res.Event)
	}
	if res.Event.OccurredAt.UTC().Format(time.RFC3339) != "2026-08-01T10:00:00Z" {
		t.Fatalf("occurred_at = %v", res.Event.OccurredAt)
	}
	// 快照已顺带写入当日。
	rows, err := st.RepoStatSnapshots().ListInRange(context.Background(), nil, "stargazers", "2026-08-01", "2026-08-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Value != 42 {
		t.Fatalf("want snapshot 42, got %+v", rows)
	}
}

// TestProcessStarDuplicateLogsDebug 相同 star 载荷二次送达（同指纹）触发事件唯一索引冲突，
// 属预期去重：Debug 留痕便于与「逻辑异常没写库」区分。
func TestProcessStarDuplicateLogsDebug(t *testing.T) {
	st := openProcessStore(t)
	var logBuffer bytes.Buffer
	p := &normalizer.Processor{
		Store:  st,
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	payload := starPayload("created", "alice", "2026-08-01T10:00:00Z", 42)
	if _, err := p.Process(context.Background(), "star", "dlv-dup-1", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Process(context.Background(), "star", "dlv-dup-2", payload); err != nil {
		t.Fatal(err)
	}
	logs := logBuffer.String()
	for _, want := range []string{`"msg":"webhook event duplicate skipped"`, `"kind":"star"`, `"action":"created"`, `"repo":`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("去重留痕应包含 %s，实际: %s", want, logs)
		}
	}
}

func TestProcessStarSameSecondDifferentUsers(t *testing.T) {
	st := openProcessStore(t)
	p := &normalizer.Processor{Store: st}
	payloadA := starPayload("created", "alice", "2026-08-01T10:00:00Z", 42)
	payloadB := starPayload("created", "bob", "2026-08-01T10:00:00Z", 43)
	if _, err := p.Process(context.Background(), "star", "dlv-1", payloadA); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Process(context.Background(), "star", "dlv-2", payloadB); err != nil {
		t.Fatal(err)
	}
	total, _, err := st.Events().List(context.Background(), store.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	starEvents := 0
	for _, ev := range total {
		if ev.Kind == store.StarKind {
			starEvents++
		}
	}
	if starEvents != 2 {
		t.Fatalf("want 2 star events, got %d", starEvents)
	}
}

func TestProcessWatchStarted(t *testing.T) {
	st := openProcessStore(t)
	p := &normalizer.Processor{Store: st}
	res, err := p.Process(context.Background(), "watch", "dlv-1", watchPayload("started", "carol"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil || res.Event.Kind != store.WatchKind || res.Event.Action != "started" || res.Event.Actor != "carol" {
		t.Fatalf("unexpected event: %+v", res.Event)
	}
}

func TestProcessStarGatedByCapability(t *testing.T) {
	st := openProcessStore(t)
	repo := store.Repository{ID: newID(), Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "r", FullName: "o/r"}
	if _, err := st.Repositories().Upsert(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	// 能力开关由 UpdateSettings 单独管理：Upsert 创建路径走 DB 默认值（true），
	// 直接在 struct 里给 StarsEnabled:false 不会落库。
	starsOff, watchesOn := false, true
	if err := st.Repositories().UpdateSettings(context.Background(), repo.ID, store.RepositorySettings{StarsEnabled: &starsOff, WatchesEnabled: &watchesOn}); err != nil {
		t.Fatal(err)
	}
	p := &normalizer.Processor{Store: st}
	res, err := p.Process(context.Background(), "star", "dlv-1", starPayload("created", "alice", "2026-08-01T10:00:00Z", 42))
	if err != nil {
		t.Fatal(err)
	}
	if res.Event != nil || !res.SuppressNotify {
		t.Fatalf("star event should be gated: %+v", res)
	}
	// 也不写快照。
	rows, _ := st.RepoStatSnapshots().ListInRange(context.Background(), nil, "stargazers", "2026-08-01", "2026-08-01")
	if len(rows) != 0 {
		t.Fatalf("gated repo should not write snapshot, got %+v", rows)
	}
}

// repository.deleted 事件：GitHub 侧仓库已删除，本地必须级联删除仓库与全部关联数据，
// 已打开的 PR/Issue、事件、告警与待投递通知不得残留。
func TestProcessRepositoryDeletedCascadeRemovesData(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)
	ctx := t.Context()

	// 预置该仓库的打开 PR 与对应事件。
	if _, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID: "wi-del", RepositoryID: repo.ID, Number: 9, Kind: store.WorkItemKindPR,
		State: "open", Title: "删除前打开的 PR", Author: "alice",
		HTMLURL: "https://github.com/acme/demo/pull/9", SourceUpdatedAt: time.Now().UTC(), StateHash: "h-del",
	}); err != nil {
		t.Fatal(err)
	}
	repoID := repo.ID
	if _, err := data.Events().Create(ctx, store.Event{
		ID: "ev-del", Source: "webhook", Kind: store.WorkItemKindPR, Action: "opened",
		RepositoryID: &repoID, Title: "删除前打开的 PR", Actor: "alice",
		OccurredAt: time.Now().UTC(), DedupeFingerprint: "fp-del", StateHash: "h-del",
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"action":     "deleted",
		"repository": demoRepoPayload(false),
	})
	proc := &normalizer.Processor{Store: data}
	if _, err := proc.Process(ctx, "repository", "delivery-deleted", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := data.Repositories().Get(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("仓库应已级联删除，got err=%v", err)
	}
	if _, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 9); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("关联 WorkItem 应已删除，got err=%v", err)
	}
	if _, err := data.Events().GetByFingerprint(ctx, "fp-del"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("关联 Event 应已删除，got err=%v", err)
	}
}

// installation_repositories 的 removed 载荷：仓库被移出安装（授权收回）。
// GitHub 侧仓库仍存在，本地保留数据但必须暂停采集（unavailable）。
func TestProcessInstallationRepositoriesRemovedMarksUnavailable(t *testing.T) {
	data := openProcessStore(t)
	repo := seedActiveDemoRepo(t, data)

	payload, _ := json.Marshal(map[string]any{
		"action": "removed",
		"installation": map[string]any{
			"id":      4242,
			"account": map[string]any{"login": "acme", "type": "Organization"},
		},
		"repositories_removed": []map[string]any{{
			"id": 99, "name": "demo", "full_name": "acme/demo",
			"owner": map[string]any{"login": "acme"},
		}},
	})
	proc := &normalizer.Processor{Store: data}
	if _, err := proc.Process(t.Context(), "installation_repositories", "delivery-removed", payload); err != nil {
		t.Fatal(err)
	}
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusUnavailable {
		t.Fatalf("removed 后应标记 unavailable，got %q", got.SyncStatus)
	}
}
