package normalizer_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
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
			"id":            999001,
			"name":          "",
			"workflow_id":   10,
			"run_number":    5,
			"event":         "push",
			"status":        "completed",
			"conclusion":    "success",
			"run_attempt":   0,
			"head_branch":   "",
			"head_sha":      "",
			"html_url":      "",
			"run_started_at": time.Now().UTC().Format(time.RFC3339),
			"updated_at":    time.Now().UTC().Format(time.RFC3339),
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
		Type:     store.RepositoryTypeInstallation,
		Owner:    "acme",
		Name:     "demo",
		FullName: "acme/demo",
		HTMLURL:  "https://github.com/acme/demo",
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
}
