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
			"number": 7,
			"title":  "hello",
			"state":  "open",
			"html_url": "https://github.com/acme/demo/issues/7",
			"user": map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels": []any{},
			"assignees": []any{},
		},
		"repository": map[string]any{
			"id": 99,
			"name": "demo",
			"full_name": "acme/demo",
			"private": false,
			"html_url": "https://github.com/acme/demo",
			"default_branch": "main",
			"owner": map[string]any{"login": "acme"},
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
