package normalizer_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// seedInstallation 经 installation.created 载荷预置本地安装行，返回 GitHub installation ID。
func seedInstallation(t *testing.T, data store.Store, ghInstallID int64) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id":      ghInstallID,
			"account": map[string]any{"login": "acme", "type": "User"},
		},
		"repositories": []any{},
	})
	proc := &normalizer.Processor{Store: data}
	if _, err := proc.Process(t.Context(), "installation", "delivery-install-"+string(rune(ghInstallID)), payload); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
}

// issuePayloadFor 构造 acme/demo 的 issues 载荷；withInstallation 控制是否携带信封 installation。
func issuePayloadFor(t *testing.T, withInstallation bool, ghInstallID int64) []byte {
	t.Helper()
	payload := map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": 7, "title": "hello", "state": "open",
			"html_url":   "https://github.com/acme/demo/issues/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{}, "assignees": []any{},
		},
		"repository": map[string]any{
			"id": 4242, "name": "demo", "full_name": "acme/demo", "private": true,
			"html_url": "https://github.com/acme/demo", "default_branch": "main",
			"owner": map[string]any{"login": "acme"},
		},
	}
	if withInstallation {
		payload["installation"] = map[string]any{"id": ghInstallID}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestProcessIssueBindsInstallationFromEnvelope 回归：非 installation 类 webhook 首建仓库时，
// 必须从信封的 installation.id 补回安装绑定。缺失该绑定时对账永远停在 baseline_sync，
// 事件被永久抑制通知（平台静默失去对该仓库的可见性）。
func TestProcessIssueBindsInstallationFromEnvelope(t *testing.T) {
	const ghInstallID int64 = 149631800
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite", URL: "file:" + filepath.Join(t.TempDir(), "bind.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	seedInstallation(t, data, ghInstallID)

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "issues", "delivery-bind-1", issuePayloadFor(t, true, ghInstallID))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if res.Repository == nil {
		t.Fatal("期望产生仓库")
	}
	if res.Repository.InstallationID == nil || *res.Repository.InstallationID == "" {
		t.Fatalf("仓库应携带安装绑定，实际为 nil/空：%+v", res.Repository)
	}
	// 绑定须指向本地安装行的内部主键，而非 GitHub installation ID。
	inst, err := data.Installations().GetByInstallationID(t.Context(), ghInstallID)
	if err != nil {
		t.Fatalf("本地安装行应存在: %v", err)
	}
	if *res.Repository.InstallationID != inst.ID {
		t.Fatalf("安装绑定应解析为本地主键 %q，实际 %q", inst.ID, *res.Repository.InstallationID)
	}
	if res.Repository.Type != store.RepositoryTypeInstallation {
		t.Fatalf("类型应为 installation，实际 %q", res.Repository.Type)
	}
}

// TestProcessIssueWithoutInstallationLogsBindingGap 对照：载荷未携带 installation 时
// 仍按既有语义落库（不阻断投递），但必须 Warn 留痕，使「永远无法对账的仓库行」可见。
func TestProcessIssueWithoutInstallationLogsBindingGap(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite", URL: "file:" + filepath.Join(t.TempDir(), "gap.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	proc := &normalizer.Processor{Store: data, Logger: logger}
	res, err := proc.Process(t.Context(), "issues", "delivery-gap-1", issuePayloadFor(t, false, 0))
	if err != nil {
		t.Fatalf("载荷缺 installation 不应阻断处理: %v", err)
	}
	if res.Repository == nil || res.Repository.InstallationID != nil {
		t.Fatalf("无 installation 时绑定应为 nil，实际 %+v", res.Repository)
	}
	if !strings.Contains(logs.String(), "repo_missing_installation_binding") {
		t.Fatalf("应 Warn 留痕 repo_missing_installation_binding，实际日志：%s", logs.String())
	}
}
