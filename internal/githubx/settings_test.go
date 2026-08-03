package githubx

import (
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// openGithubTestStore 打开内存 SQLite 测试库。
func openGithubTestStore(t *testing.T) store.Store {
	t.Helper()
	dbURL := "file:" + filepath.Join(t.TempDir(), "githubx.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

// newTestKeyRing 构造当前+上一把密钥的密钥环。
func newTestKeyRing(t *testing.T) cryptox.KeyRing {
	t.Helper()
	cur := make([]byte, 32)
	prev := make([]byte, 32)
	for i := range cur {
		cur[i] = byte(i + 1)
		prev[i] = byte(255 - i)
	}
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey:  config.NewSecret(hex.EncodeToString(cur)),
		PreviousKey: config.NewSecret(hex.EncodeToString(prev)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

// TestSettingsLoadStoredRuntime 验证未配置时返回零值。
func TestSettingsLoadStoredRuntime(t *testing.T) {
	data := openGithubTestStore(t)
	stored, err := LoadStoredRuntime(t.Context(), data)
	if err != nil {
		t.Fatalf("LoadStoredRuntime: %v", err)
	}
	if stored != (StoredRuntime{}) {
		t.Fatalf("空库应返回零值, got %+v", stored)
	}

	// nil store 安全。
	if s, err := LoadStoredRuntime(t.Context(), nil); err != nil || s != (StoredRuntime{}) {
		t.Fatalf("nil store 应返回零值, got %+v err=%v", s, err)
	}
}

// TestSettingsSaveLoadRoundTrip 验证 Save 后能完整读回。
func TestSettingsSaveLoadRoundTrip(t *testing.T) {
	data := openGithubTestStore(t)
	want := StoredRuntime{
		AppID:          42,
		ClientID:       "Iv1.client",
		PrivateKeyPath: "/keys/app.pem",
		PublicBaseURL:  "https://example.org",
	}
	if err := SaveStoredRuntime(t.Context(), data, want); err != nil {
		t.Fatalf("SaveStoredRuntime: %v", err)
	}
	got, err := LoadStoredRuntime(t.Context(), data)
	if err != nil {
		t.Fatalf("LoadStoredRuntime: %v", err)
	}
	if got.AppID != want.AppID || got.ClientID != want.ClientID || got.PrivateKeyPath != want.PrivateKeyPath {
		t.Fatalf("round-trip 不一致: got %+v want %+v", got, want)
	}
	if got.PublicBaseURL != want.PublicBaseURL {
		t.Fatalf("PublicBaseURL = %q, want %q", got.PublicBaseURL, want.PublicBaseURL)
	}

	// 再次 Save 应覆盖（Upsert 语义）。
	updated := want
	updated.ClientID = "Iv1.updated"
	if err := SaveStoredRuntime(t.Context(), data, updated); err != nil {
		t.Fatalf("二次 Save: %v", err)
	}
	got2, _ := LoadStoredRuntime(t.Context(), data)
	if got2.ClientID != "Iv1.updated" {
		t.Fatalf("Upsert 未覆盖, got %q", got2.ClientID)
	}
}

// TestSettingsEncryptDecryptRoundTrip 验证密钥环加密/解密闭环。
func TestSettingsEncryptDecryptRoundTrip(t *testing.T) {
	ring := newTestKeyRing(t)
	plain := "ghp_secret_token_123"
	env, err := EncryptSecret(t.Context(), &ring, plain)
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if env == "" || strings.Contains(env, plain) {
		t.Fatalf("信封不应包含明文: %q", env)
	}
	got, err := DecryptSecret(t.Context(), &ring, env)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if got != plain {
		t.Fatalf("解密不匹配: got %q want %q", got, plain)
	}

	// nil keyRing 报错。
	if _, err := EncryptSecret(t.Context(), nil, plain); err == nil {
		t.Fatal("nil keyRing 加密应报错")
	}
	if _, err := DecryptSecret(t.Context(), nil, env); err == nil {
		t.Fatal("nil keyRing 解密应报错")
	}
	// 非法信封报错。
	if _, err := DecryptSecret(t.Context(), &ring, "not-an-envelope"); err == nil {
		t.Fatal("非法信封应报错")
	}
}

// TestSettingsMergeFromStore 验证数据库值填充 env 未设置的字段。
func TestSettingsMergeFromStore(t *testing.T) {
	data := openGithubTestStore(t)
	ring := newTestKeyRing(t)

	envPEM, err := EncryptSecret(t.Context(), &ring, "-----BEGIN RSA PRIVATE KEY-----\nFAKE\n-----END RSA PRIVATE KEY-----")
	if err != nil {
		t.Fatal(err)
	}
	envWebhook, err := EncryptSecret(t.Context(), &ring, "webhook-secret-1")
	if err != nil {
		t.Fatal(err)
	}

	stored := StoredRuntime{
		AppID:                 7,
		ClientID:              "Iv1.db",
		PrivateKeyPEMEnvelope: envPEM,
		WebhookSecretEnvelope: envWebhook,
		PublicBaseURL:         "https://db.example.org",
	}
	if err := SaveStoredRuntime(t.Context(), data, stored); err != nil {
		t.Fatal(err)
	}

	// env 已提供 AppID/ClientID，数据库不应覆盖它们。
	rt := &RuntimeConfig{AppID: 1, ClientID: "Iv1.env", Client: &AppClient{}}
	if err := MergeFromStore(t.Context(), data, &ring, rt); err != nil {
		t.Fatalf("MergeFromStore: %v", err)
	}
	if rt.AppID != 1 {
		t.Fatalf("env AppID 被覆盖: %d", rt.AppID)
	}
	if rt.ClientID != "Iv1.env" {
		t.Fatalf("env ClientID 被覆盖: %q", rt.ClientID)
	}
	// 数据库应填充 PEM 与 webhook secret。
	if !strings.Contains(rt.PrivateKeyPEM, "FAKE") {
		t.Fatalf("PEM 未从数据库合并: %q", rt.PrivateKeyPEM)
	}
	if rt.PrivateKeySource != "database" {
		t.Fatalf("PrivateKeySource = %q, want database", rt.PrivateKeySource)
	}
	if rt.WebhookSecret != "webhook-secret-1" {
		t.Fatalf("WebhookSecret 未合并: %q", rt.WebhookSecret)
	}
	if rt.WebhookSecretSource != "database" {
		t.Fatalf("WebhookSecretSource = %q, want database", rt.WebhookSecretSource)
	}
	if rt.PublicBaseURL != "https://db.example.org" {
		t.Fatalf("PublicBaseURL 未合并: %q", rt.PublicBaseURL)
	}
	if rt.Client == nil {
		t.Fatal("MergeFromStore 不应清空 Client")
	}
}

// TestSettingsMergeFromStoreEmptyStore 验证无数据库配置时保持 env 值。
func TestSettingsMergeFromStoreEmptyStore(t *testing.T) {
	data := openGithubTestStore(t)
	ring := newTestKeyRing(t)
	rt := &RuntimeConfig{AppID: 9, ClientID: "Iv1.env", Client: &AppClient{}}
	if err := MergeFromStore(t.Context(), data, &ring, rt); err != nil {
		t.Fatalf("MergeFromStore: %v", err)
	}
	if rt.AppID != 9 || rt.ClientID != "Iv1.env" {
		t.Fatalf("空库不应改变 env 配置: %+v", rt)
	}

	// nil data / nil rt 安全。
	if err := MergeFromStore(t.Context(), nil, &ring, rt); err != nil {
		t.Fatalf("nil data: %v", err)
	}
	if err := MergeFromStore(t.Context(), data, &ring, nil); err != nil {
		t.Fatalf("nil rt: %v", err)
	}
}

// TestSettingsMergeFromStoreUnrelatedSetting 验证存储中其他键/未知结构不影响合并。
func TestSettingsMergeFromStoreUnrelatedSetting(t *testing.T) {
	data := openGithubTestStore(t)
	ring := newTestKeyRing(t)
	// 未知字段结构的合法 JSON：LoadStoredRuntime 应忽略未知字段并返回零值。
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       RuntimeSettingKey,
		ValueJSON: json.RawMessage(`{"unexpected_field": 123}`),
	}); err != nil {
		t.Fatal(err)
	}
	rt := &RuntimeConfig{AppID: 3, Client: &AppClient{}}
	if err := MergeFromStore(t.Context(), data, &ring, rt); err != nil {
		t.Fatalf("MergeFromStore: %v", err)
	}
	if rt.AppID != 3 {
		t.Fatalf("AppID 被改动: %d", rt.AppID)
	}
}
