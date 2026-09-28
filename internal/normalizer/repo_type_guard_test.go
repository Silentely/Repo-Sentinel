package normalizer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func newRepoSource(fullName string) *ghRepository {
	name := fullName
	if idx := strings.IndexByte(fullName, '/'); idx >= 0 {
		name = fullName[idx+1:]
	}
	return &ghRepository{
		ID: 202, Name: name, FullName: fullName,
		HTMLURL: "https://github.com/" + fullName, DefaultBranch: "main",
		Owner: struct {
			Login string `json:"login"`
		}{Login: "octo"},
	}
}

// TestNormalizeRepositoryKeepsTypeWithoutInstallationBinding 回归：webhook 归一化不得在没有
// 安装绑定时把既有仓库改判为 installation。改判后该行会退出匿名轮询，却仍在
// resolveInstallationToken 处失败，sync_status 永远停在 baseline_sync。
func TestNormalizeRepositoryKeepsTypeWithoutInstallationBinding(t *testing.T) {
	data := openStore(t)
	ctx := context.Background()

	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ext-type", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "octo", Name: "pub", FullName: "octo/pub",
		HTMLURL: "https://github.com/octo/pub",
	}); err != nil {
		t.Fatalf("seed external repo: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	repo, err := NormalizeRepository(ctx, data, newRepoSource("octo/pub"), nil, logger)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if repo.Type != store.RepositoryTypeExternal {
		t.Fatalf("无安装绑定时类型应保持 external_public，实际 %q", repo.Type)
	}
	loaded, err := data.Repositories().Get(ctx, "repo-ext-type")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Type != store.RepositoryTypeExternal {
		t.Fatalf("库内类型被改判为 %q", loaded.Type)
	}
	if !strings.Contains(logs.String(), "repo_type_reclassify_skipped") {
		t.Fatalf("改判被跳过必须留痕，实际日志: %s", logs.String())
	}
}

// TestNormalizeRepositoryUpgradesTypeWithInstallationBinding 对照：载荷携带可解析的安装
// 绑定时允许升级为 installation（安装事件与 installation 清单同步走此路径）。
func TestNormalizeRepositoryUpgradesTypeWithInstallationBinding(t *testing.T) {
	data := openStore(t)
	ctx := context.Background()

	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ext-upgrade", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "octo", Name: "priv", FullName: "octo/priv",
		HTMLURL: "https://github.com/octo/priv",
	}); err != nil {
		t.Fatalf("seed external repo: %v", err)
	}

	installationID := "installation-42"
	repo, err := NormalizeRepository(ctx, data, newRepoSource("octo/priv"), &installationID, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if repo.Type != store.RepositoryTypeInstallation {
		t.Fatalf("携带安装绑定应升级为 installation，实际 %q", repo.Type)
	}
	if repo.InstallationID == nil || *repo.InstallationID != installationID {
		t.Fatalf("安装绑定未写入: %+v", repo.InstallationID)
	}
}
