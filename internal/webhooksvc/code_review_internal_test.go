package webhooksvc

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// openReviewInternalStore 为内部（同包）测试打开隔离的 SQLite 库：
// Atlas 使用 sql.DB 时取 TMPDIR 下的固定锁名，须每个测试独立目录。
func openReviewInternalStore(t *testing.T) store.Store {
	t.Helper()
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(temporaryDir, "webhooksvc-internal.db"),
	})
	if err != nil {
		t.Fatalf("打开内部测试 Store 失败: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func seedInstallation(t *testing.T, data store.Store, id string, installationID int64, account string) store.GitHubInstallation {
	t.Helper()
	row, err := data.Installations().Upsert(context.Background(), store.GitHubInstallation{
		ID:             id,
		InstallationID: installationID,
		AccountLogin:   account,
		AccountType:    "Organization",
		TargetType:     "Organization",
	})
	if err != nil {
		t.Fatalf("seed installation %d: %v", installationID, err)
	}
	return row
}

// TestResolveRepoInstallationID 覆盖四条解析路径：
// 内部 ULID → 安装记录、纯数字 → 直接安装编号、无 installation_id → 单 App 回退、
// 无安装信息与多安装歧义 → 0（下游以「无令牌」安全拒绝）。
func TestResolveRepoInstallationID(t *testing.T) {
	t.Run("内部 ULID 解析为安装记录", func(t *testing.T) {
		data := openReviewInternalStore(t)
		row := seedInstallation(t, data, "inst-ulid", 4242, "acme")
		svc := &Service{Store: data}

		raw := row.ID
		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo", InstallationID: &raw})
		if got != 4242 {
			t.Fatalf("ULID 安装引用应解析为 4242，got %d", got)
		}
	})

	t.Run("纯数字按安装编号解析", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedInstallation(t, data, "inst-numeric", 777, "acme")
		svc := &Service{Store: data}

		raw := "777"
		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo", InstallationID: &raw})
		if got != 777 {
			t.Fatalf("纯数字 installation_id 应解析为 777，got %d", got)
		}
	})

	t.Run("未知安装编号回退为原值", func(t *testing.T) {
		data := openReviewInternalStore(t)
		svc := &Service{Store: data}

		raw := "999"
		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo", InstallationID: &raw})
		if got != 999 {
			t.Fatalf("未登记的安装编号应按原值返回 999，got %d", got)
		}
	})

	t.Run("单 App 部署回退并留痕", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedInstallation(t, data, "inst-single", 5150, "acme")
		logs := &bytes.Buffer{}
		svc := &Service{Store: data, Logger: slog.New(slog.NewJSONHandler(logs, nil))}

		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo"})
		if got != 5150 {
			t.Fatalf("单安装回退应返回 5150，got %d", got)
		}
		if !strings.Contains(logs.String(), "installation id resolved via single-app fallback") ||
			!strings.Contains(logs.String(), "acme/demo") {
			t.Fatalf("单 App 回退应留 Warn 留痕，got %s", logs.String())
		}
	})

	t.Run("多安装歧义返回 0", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedInstallation(t, data, "inst-a", 1001, "acme")
		seedInstallation(t, data, "inst-b", 1002, "beta")
		svc := &Service{Store: data}

		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo"})
		if got != 0 {
			t.Fatalf("存在多条安装记录且无仓库引用时应返回 0，got %d", got)
		}
	})

	t.Run("安装表为空返回 0", func(t *testing.T) {
		data := openReviewInternalStore(t)
		svc := &Service{Store: data}

		got := svc.resolveRepoInstallationID(context.Background(), store.Repository{FullName: "acme/demo"})
		if got != 0 {
			t.Fatalf("无安装记录时应返回 0，got %d", got)
		}
	})

	t.Run("未注入 Store 返回 0", func(t *testing.T) {
		raw := "777"
		svc := &Service{}
		if got := svc.resolveRepoInstallationID(context.Background(), store.Repository{InstallationID: &raw}); got != 0 {
			t.Fatalf("无 Store 时应返回 0，got %d", got)
		}
	})
}

// seedPRReview 写入指定 head SHA 的审查结果，模拟已审查状态。
func seedPRReview(t *testing.T, data store.Store, itemID, headSHA string, commented bool) {
	t.Helper()
	payload, err := json.Marshal(ai.CodeReviewResult{
		Summary:       "ok",
		Score:         90,
		HeadSHA:       headSHA,
		CommentedOnPR: commented,
	})
	if err != nil {
		t.Fatalf("marshal review: %v", err)
	}
	if _, err := data.Settings().Upsert(context.Background(), store.SystemSetting{
		ID:        "setting-" + itemID,
		Key:       "ai.pr_review." + itemID,
		ValueJSON: payload,
		UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("upsert review setting: %v", err)
	}
}

// TestReviewAlreadyStored 锁定「同一 head SHA 已审查即跳过」的幂等判定：
// 命中同 SHA、换 SHA 需重审、缺设置/空 SHA/空 itemID/坏 JSON/无 Store 一律不跳过。
func TestReviewAlreadyStored(t *testing.T) {
	t.Run("同 head SHA 判定已存储", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedPRReview(t, data, "item-1", "sha-same", true)
		svc := &Service{Store: data}

		if !svc.reviewAlreadyStored(context.Background(), "item-1", "sha-same") {
			t.Fatalf("同一 head SHA 应判定为已存储")
		}
	})

	t.Run("不同 head SHA 需重新审查", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedPRReview(t, data, "item-1", "sha-old", true)
		svc := &Service{Store: data}

		if svc.reviewAlreadyStored(context.Background(), "item-1", "sha-new") {
			t.Fatalf("换 head SHA 后不应判定为已存储")
		}
	})

	t.Run("无审查记录不跳过", func(t *testing.T) {
		data := openReviewInternalStore(t)
		svc := &Service{Store: data}

		if svc.reviewAlreadyStored(context.Background(), "item-missing", "sha-new") {
			t.Fatalf("无审查记录时不应判定为已存储")
		}
	})

	t.Run("空 head SHA 不跳过", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedPRReview(t, data, "item-1", "sha-old", true)
		svc := &Service{Store: data}

		if svc.reviewAlreadyStored(context.Background(), "item-1", "   ") {
			t.Fatalf("head SHA 为空白时不应判定为已存储")
		}
	})

	t.Run("空 itemID 不跳过", func(t *testing.T) {
		data := openReviewInternalStore(t)
		seedPRReview(t, data, "item-1", "sha-old", true)
		svc := &Service{Store: data}

		if svc.reviewAlreadyStored(context.Background(), "", "sha-old") {
			t.Fatalf("itemID 为空时不应判定为已存储")
		}
	})

	t.Run("审查结果不可解析不跳过", func(t *testing.T) {
		data := openReviewInternalStore(t)
		if _, err := data.Settings().Upsert(context.Background(), store.SystemSetting{
			ID:  "setting-item-broken",
			Key: "ai.pr_review.item-broken",
			// 合法 JSON 但字段类型与 CodeReviewResult 不符，写库通过而反序列化失败。
			ValueJSON: []byte(`{"score":"high"}`),
			UpdatedBy: "test",
		}); err != nil {
			t.Fatalf("upsert broken review setting: %v", err)
		}
		svc := &Service{Store: data}

		if svc.reviewAlreadyStored(context.Background(), "item-broken", "sha-old") {
			t.Fatalf("审查结果不可解析时不应判定为已存储")
		}
	})

	t.Run("未注入 Store 不跳过", func(t *testing.T) {
		svc := &Service{}
		if svc.reviewAlreadyStored(context.Background(), "item-1", "sha-old") {
			t.Fatalf("无 Store 时不应判定为已存储")
		}
	})
}
