package ai

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func testKeyRing(t *testing.T) *cryptox.KeyRing {
	t.Helper()
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(make([]byte, 32))),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &ring
}

func openRuntimeStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "ai-runtime.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

// RuntimeFromEnv 应标记字段来源：显式设置的字段 env，默认值字段 unset。
func TestRuntimeFromEnvSources(t *testing.T) {
	// DigestEnabled 为默认值 true（未显式偏离）、TriageEnabled 显式关闭。
	rt := RuntimeFromEnv(config.AIConfig{
		Enabled: true, BaseURL: "http://127.0.0.1:11434/v1", Model: "llama3.1",
		Timeout: 30 * time.Second, MaxTokens: 1024, APIKey: config.NewSecret("sk-env"),
		DigestEnabled: true, TriageEnabled: false,
	})
	if rt.EnabledSource != "env" || rt.BaseURLSource != "env" || rt.ModelSource != "env" ||
		rt.TimeoutSource != "env" || rt.MaxTokensSource != "env" || rt.APIKeySource != "env" {
		t.Fatalf("显式 env 字段应标记 env：%+v", rt)
	}
	if rt.DigestEnabledSource != "unset" {
		t.Fatalf("digest 默认值不应视为显式设置：%s", rt.DigestEnabledSource)
	}
	if rt.TriageEnabledSource != "env" {
		t.Fatalf("triage 显式关闭应标记 env：%s", rt.TriageEnabledSource)
	}

	// 全默认配置（bool 均为默认值）下字段应可被管理台编辑。
	empty := RuntimeFromEnv(config.AIConfig{DigestEnabled: true, TriageEnabled: true})
	if empty.EnabledSource != "unset" || empty.APIKeySource != "unset" || empty.BaseURLSource != "unset" ||
		empty.DigestEnabledSource != "unset" || empty.TriageEnabledSource != "unset" {
		t.Fatalf("全默认配置应全部 unset：%+v", empty)
	}
}

// MergeFromStore：env 未设置的字段用 DB 补缺，env 已设置的字段不被 DB 覆盖。
func TestMergeFromStore(t *testing.T) {
	data := openRuntimeStore(t)
	ring := testKeyRing(t)

	env, err := EncryptAPIKey(t.Context(), ring, "sk-db")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	off := false
	timeout := int64(45)
	maxTokens := 512
	if err := SaveStoredConfig(t.Context(), data, StoredConfig{
		Enabled: &on, BaseURL: "http://db.example/v1", Model: "db-model",
		TimeoutSec: &timeout, MaxTokens: &maxTokens, APIKeyEnvelope: env,
		DigestEnabled: &off, TriageEnabled: &on,
	}); err != nil {
		t.Fatal(err)
	}

	// env 只设置了 BaseURL 与 Model，其余由 DB 补缺（bool 保持默认值以免被判定为显式设置）。
	rt := RuntimeFromEnv(config.AIConfig{BaseURL: "http://env.example/v1", Model: "env-model", DigestEnabled: true, TriageEnabled: true})
	if err := MergeFromStore(t.Context(), data, ring, rt); err != nil {
		t.Fatal(err)
	}
	snap := rt.Snapshot()
	if snap.BaseURL != "http://env.example/v1" || snap.BaseURLSource != "env" {
		t.Fatalf("env 已设置的 BaseURL 不应被 DB 覆盖：%+v", snap)
	}
	if snap.Model != "env-model" || snap.ModelSource != "env" {
		t.Fatalf("env 已设置的 Model 不应被 DB 覆盖：%+v", snap)
	}
	if !snap.Enabled || snap.EnabledSource != "database" {
		t.Fatalf("enabled 应由 DB 补缺：%+v", snap)
	}
	if snap.APIKey != "sk-db" || snap.APIKeySource != "database" {
		t.Fatalf("API Key 应解密自 DB：%+v", snap)
	}
	if snap.Timeout != 45*time.Second || snap.MaxTokens != 512 {
		t.Fatalf("timeout/max_tokens 应由 DB 补缺：%+v", snap)
	}
	if snap.DigestEnabled || !snap.TriageEnabled {
		t.Fatalf("digest/triage 布尔应由 DB 补缺：%+v", snap)
	}
	if snap.DigestEnabledSource != "database" || snap.TriageEnabledSource != "database" {
		t.Fatalf("digest/triage 来源应为 database：%+v", snap)
	}
}

// 密钥信封往返：加密后不可见明文，解密还原。
func TestEncryptDecryptAPIKeyRoundTrip(t *testing.T) {
	ring := testKeyRing(t)
	env, err := EncryptAPIKey(t.Context(), ring, "sk-plain")
	if err != nil {
		t.Fatal(err)
	}
	if env == "" || env == "sk-plain" {
		t.Fatal("信封不应为空或等于明文")
	}
	plain, err := DecryptAPIKey(t.Context(), ring, env)
	if err != nil || plain != "sk-plain" {
		t.Fatalf("解密还原失败：plain=%q err=%v", plain, err)
	}
}
