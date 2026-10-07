package rules

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// 打桩 GitHub 客户端：接受 InstallationToken 与 AddIssueLabels 请求，并记录打标次数。
// 需要真实 App 私钥：签发 installation token 第一步是签 App JWT，无私钥会直接失败。
func newAutoLabelGHStub(t *testing.T, addCalls *int) *githubx.AppClient {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghu_stub","expires_at":"2030-01-01T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/labels"):
			*addCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"sentinel:bug"}]`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(fake.Close)
	client := githubx.NewAppClient(1234, keyPath)
	client.BaseURL = fake.URL
	return client
}

// openAutoLabelStore 打开用例独立库：DSN 必须唯一，迁移锁名由 DSN 派生，
// 复用同一 DSN 会让并行测试进程争用同一把锁而误报迁移失败。
func openAutoLabelStore(t *testing.T) store.Store {
	t.Helper()
	opened, err := store.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "auto-label.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func newAutoLabelEngine(t *testing.T, addCalls *int) (*Engine, store.Store) {
	t.Helper()
	ctx := context.Background()
	opened := openAutoLabelStore(t)
	if _, err := opened.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-auto-label",
		FullName:       "org/auto-label",
		InstallationID: strPtr("999"),
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	if _, err := opened.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-auto-label-enabled",
		Key:       "ai.auto_label_enabled",
		ValueJSON: []byte(`true`),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enable auto label: %v", err)
	}
	return &Engine{Store: opened, GitHub: newAutoLabelGHStub(t, addCalls)}, opened
}

func strPtr(s string) *string { return &s }

func triageBug() *ai.IssueTriageResult {
	return &ai.IssueTriageResult{Category: "Bug Report"}
}

// TestMaybeAutoLabelIssue_Idempotent 多次触发同一 Issue 的分诊只打一次标：回执键竞争防重。
func TestMaybeAutoLabelIssue_Idempotent(t *testing.T) {
	addCalls := 0
	engine, data := newAutoLabelEngine(t, &addCalls)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		engine.maybeAutoLabelIssue(ctx, "org/auto-label", 7, triageBug())
	}
	if addCalls != 1 {
		t.Fatalf("期望只调用一次打标接口，实际 %d 次", addCalls)
	}

	receiptKey := store.AutoLabelReceiptKey("org/auto-label", 7, triageBug().Category)
	if _, err := data.Settings().Get(ctx, receiptKey); err != nil {
		t.Fatalf("打标成功后应留存回执键: %v", err)
	}
}

// TestMaybeAutoLabelIssue_Concurrent 并发分诊同一 Issue 只允许一个调用打标接口。
// 回执键用唯一约束竞争，而非先读后写。
func TestMaybeAutoLabelIssue_Concurrent(t *testing.T) {
	addCalls := 0
	engine, _ := newAutoLabelEngine(t, &addCalls)
	ctx := context.Background()

	const workers = 8
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			engine.maybeAutoLabelIssue(ctx, "org/auto-label", 8, triageBug())
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	if addCalls != 1 {
		t.Fatalf("并发分诊应当只有一次打标调用，实际 %d 次", addCalls)
	}
}

// TestMaybeAutoLabelIssue_FailureReleasesReceipt 打标失败后回执键被回收，下次分诊可重试。
func TestMaybeAutoLabelIssue_FailureReleasesReceipt(t *testing.T) {
	ctx := context.Background()
	opened := openAutoLabelStore(t)
	if _, err := opened.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-no-install", FullName: "org/no-install", MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	if _, err := opened.Settings().Upsert(ctx, store.SystemSetting{
		ID: "s-al-enabled", Key: "ai.auto_label_enabled", ValueJSON: []byte(`true`), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enable auto label: %v", err)
	}
	// 无 InstallationID，GetByFullName 后 early return：回执键必须被 release。
	engine := &Engine{Store: opened}

	engine.maybeAutoLabelIssue(ctx, "org/no-install", 5, triageBug())
	receiptKey := store.AutoLabelReceiptKey("org/no-install", 5, triageBug().Category)
	if _, err := opened.Settings().Get(ctx, receiptKey); err == nil {
		t.Fatal("打标失败后回执键应被回收，允许下次重试")
	}
}

// TestCleanupTransientSettings_AutoLabelReceipt 过期的自动打标回执被清理。
func TestCleanupTransientSettings_AutoLabelReceipt(t *testing.T) {
	ctx := context.Background()
	data := openAutoLabelStore(t)
	now := time.Now().UTC()

	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: "r1", Key: store.AutoLabelReceiptKey("org/old", 1, "bug"),
		ValueJSON: []byte(`{"status":"applied"}`), UpdatedAt: now.Add(-100 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("seed old receipt: %v", err)
	}
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: "r2", Key: store.AutoLabelReceiptKey("org/fresh", 2, "bug"),
		ValueJSON: []byte(`{"status":"applied"}`), UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed fresh receipt: %v", err)
	}

	deleted, err := data.CleanupTransientSettings(ctx, now)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("期望只清理过期回执，实际删除 %d", deleted)
	}
	if _, err := data.Settings().Get(ctx, store.AutoLabelReceiptKey("org/fresh", 2, "bug")); err != nil {
		t.Fatalf("新回执不应被清理: %v", err)
	}
}

// TestMaybeAutoLabelIssue_LegacyReceiptSkip 存量兼容：升级前 github_label: 前缀回执命中时
// 不再重复调用打标接口（旧键由 CleanupTransientSettings 按保留期回收）。
func TestMaybeAutoLabelIssue_LegacyReceiptSkip(t *testing.T) {
	addCalls := 0
	engine, data := newAutoLabelEngine(t, &addCalls)
	ctx := context.Background()

	// 预置迁移前格式的回执键
	legacyKey := store.LegacyAutoLabelReceiptKey("org/auto-label", 9, triageBug().Category)
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: "legacy-receipt", Key: legacyKey, ValueJSON: []byte(`{"state":"succeeded"}`), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed legacy receipt: %v", err)
	}

	engine.maybeAutoLabelIssue(ctx, "org/auto-label", 9, triageBug())

	if addCalls != 0 {
		t.Fatalf("命中存量回执时不应再打标，实际调用 %d 次", addCalls)
	}
}

// TestMaybeAutoLabelIssue_ULIDInstallationResolution 验证当仓库 InstallationID 存储为内部表 ULID 主键时，
// engine 能正确从 installations 表反查真实的 GitHub 数字 InstallationID 并顺利完成打标。
func TestMaybeAutoLabelIssue_ULIDInstallationResolution(t *testing.T) {
	addCalls := 0
	ctx := context.Background()
	opened := openAutoLabelStore(t)

	// 创建真实的 GitHubInstallation 记录，ID 为 ULID 主键
	const ulidID = "01M4AUTOINSTALLID0000000001"
	const githubAppInstID = int64(123456)
	if _, err := opened.Installations().Upsert(ctx, store.GitHubInstallation{
		ID:             ulidID,
		InstallationID: githubAppInstID,
		AccountLogin:   "Silentely",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("upsert installation: %v", err)
	}

	// 仓库的 InstallationID 关联该 ULID 主键（复现线上数据模型）
	if _, err := opened.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-ulid-test",
		FullName:       "Silentely/TG-SignPulse",
		InstallationID: strPtr(ulidID),
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	if _, err := opened.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "s-auto-label-enabled",
		Key:       "ai.auto_label_enabled",
		ValueJSON: []byte(`true`),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enable auto label: %v", err)
	}

	engine := &Engine{
		Store:  opened,
		GitHub: newAutoLabelGHStub(t, &addCalls),
	}

	// 验证 resolveRepoInstallationID 正确解析出了 int64 数字 ID
	repoRec, err := opened.Repositories().GetByFullName(ctx, "Silentely/TG-SignPulse")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	resolvedID := engine.resolveRepoInstallationID(ctx, repoRec)
	if resolvedID != githubAppInstID {
		t.Fatalf("expected resolved ID %d, got %d", githubAppInstID, resolvedID)
	}

	// 执行 auto label，验证成功调用 GitHub 打标接口
	engine.maybeAutoLabelIssue(ctx, "Silentely/TG-SignPulse", 12, triageBug())
	if addCalls != 1 {
		t.Fatalf("expected 1 add label call, got %d", addCalls)
	}

	receiptKey := store.AutoLabelReceiptKey("Silentely/TG-SignPulse", 12, triageBug().Category)
	if _, err := opened.Settings().Get(ctx, receiptKey); err != nil {
		t.Fatalf("expected receipt to exist after successful labeling: %v", err)
	}
}

// TestResolveRepoInstallationID_NoFallbackForExplicitInvalidRef 验证当仓库显式设置了未知的 InstallationID（如无效或已删除的 ULID）时，
// 即使当前系统只存在一个全局唯一的 GitHub 安装，也绝对禁止跨租户兜底回退，必须返回 0。
func TestResolveRepoInstallationID_NoFallbackForExplicitInvalidRef(t *testing.T) {
	ctx := context.Background()
	opened := openAutoLabelStore(t)

	// 创建唯一一个合法的安装记录
	if _, err := opened.Installations().Upsert(ctx, store.GitHubInstallation{
		ID:             "01VALIDINSTALL000000000001",
		InstallationID: 999888,
		AccountLogin:   "other-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("upsert installation: %v", err)
	}

	engine := &Engine{Store: opened}

	// 1. 显式指定了无效 ULID -> 禁止回退，必须返回 0
	invalidRepo := store.Repository{
		FullName:       "org/stale-repo",
		InstallationID: strPtr("01DELETEDULID0000000000000"),
	}
	if got := engine.resolveRepoInstallationID(ctx, invalidRepo); got != 0 {
		t.Fatalf("expected 0 for explicit invalid installation id, got %d", got)
	}

	// 2. 显式指定了不存在的数字 ID -> 禁止回退，返回原数字 ID 还是 0？返回 0
	invalidNumRepo := store.Repository{
		FullName:       "org/stale-repo",
		InstallationID: strPtr("not-a-number"),
	}
	if got := engine.resolveRepoInstallationID(ctx, invalidNumRepo); got != 0 {
		t.Fatalf("expected 0 for malformed string installation id, got %d", got)
	}

	// 3. 未指定 InstallationID (nil) -> 允许作为单租户唯一安装兜底
	nilRepo := store.Repository{
		FullName:       "org/new-repo",
		InstallationID: nil,
	}
	if got := engine.resolveRepoInstallationID(ctx, nilRepo); got != 999888 {
		t.Fatalf("expected fallback to 999888 for nil installation id, got %d", got)
	}
}
