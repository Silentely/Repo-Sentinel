package ai

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
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
	empty := RuntimeFromEnv(config.AIConfig{Retries: 1, DigestEnabled: true, TriageEnabled: true})
	if empty.EnabledSource != "unset" || empty.APIKeySource != "unset" || empty.BaseURLSource != "unset" ||
		empty.DigestEnabledSource != "unset" || empty.TriageEnabledSource != "unset" || empty.RetriesSource != "unset" {
		t.Fatalf("全默认配置应全部 unset：%+v", empty)
	}
}

// 生产路径回归：配置加载器不再注入 AI 标量默认值后，RuntimeFromEnv 应把
// base_url/model/timeout/max_tokens 标记为 unset（管理台可编辑），
// 而非误判为 env 锁定导致四个字段永远无法在管理台修改。
func TestRuntimeFromLoadedDefaults(t *testing.T) {
	cfg, err := config.Load(t.Context(), config.LoadOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}
	rt := RuntimeFromEnv(cfg.AI)
	snap := rt.Snapshot()
	checks := []struct {
		name   string
		source string
	}{
		{"base_url", snap.BaseURLSource},
		{"model", snap.ModelSource},
		{"timeout", snap.TimeoutSource},
		{"max_tokens", snap.MaxTokensSource},
	}
	for _, c := range checks {
		if c.source != "unset" {
			t.Fatalf("%s 默认来源应 unset（管理台可编辑），实际 %s", c.name, c.source)
		}
	}

	// 显式设置环境变量时，这四个字段仍应标记 env（锁定，不可在管理台覆盖）。
	envCfg, err := config.Load(t.Context(), config.LoadOptions{
		LookupEnv: func(name string) (string, bool) {
			switch name {
			case "REPOSENTINEL_AI_BASE_URL":
				return "http://env.example/v1", true
			case "REPOSENTINEL_AI_MODEL":
				return "env-model", true
			case "REPOSENTINEL_AI_TIMEOUT":
				return "45s", true
			case "REPOSENTINEL_AI_MAX_TOKENS":
				return "1024", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("加载 env 配置失败: %v", err)
	}
	envSnap := RuntimeFromEnv(envCfg.AI).Snapshot()
	for _, c := range checks {
		source := map[string]string{
			"base_url":   envSnap.BaseURLSource,
			"model":      envSnap.ModelSource,
			"timeout":    envSnap.TimeoutSource,
			"max_tokens": envSnap.MaxTokensSource,
		}[c.name]
		if source != "env" {
			t.Fatalf("%s 显式 env 设置应标记 env（锁定），实际 %s", c.name, source)
		}
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
	retries := 3
	if err := SaveStoredConfig(t.Context(), data, StoredConfig{
		Enabled: &on, BaseURL: "http://db.example/v1", Model: "db-model",
		TimeoutSec: &timeout, MaxTokens: &maxTokens, Retries: &retries, APIKeyEnvelope: env,
		DigestEnabled: &off, TriageEnabled: &on,
	}); err != nil {
		t.Fatal(err)
	}

	// env 只设置了 BaseURL 与 Model，其余由 DB 补缺（bool/Retries 保持默认值以免被判定为显式设置）。
	rt := RuntimeFromEnv(config.AIConfig{BaseURL: "http://env.example/v1", Model: "env-model", Retries: 1, DigestEnabled: true, TriageEnabled: true})
	if err := MergeFromStore(t.Context(), data, ring, rt); err != nil {
		t.Fatal(err)
	}
	snap := rt.Snapshot()
	if snap.BaseURL != "http://env.example/v1" || snap.BaseURLSource != "env" {
		t.Fatalf("env 已设置的 BaseURL 不应被 DB 覆盖：%+v", &snap)
	}
	if snap.Model != "env-model" || snap.ModelSource != "env" {
		t.Fatalf("env 已设置的 Model 不应被 DB 覆盖：%+v", &snap)
	}
	if !snap.Enabled || snap.EnabledSource != "database" {
		t.Fatalf("enabled 应由 DB 补缺：%+v", &snap)
	}
	if snap.APIKey != "sk-db" || snap.APIKeySource != "database" {
		t.Fatalf("API Key 应解密自 DB：%+v", &snap)
	}
	if snap.Timeout != 45*time.Second || snap.MaxTokens != 512 {
		t.Fatalf("timeout/max_tokens 应由 DB 补缺：%+v", &snap)
	}
	if snap.Retries != 3 || snap.RetriesSource != "database" {
		t.Fatalf("retries 应由 DB 补缺：%+v", &snap)
	}
	if snap.DigestEnabled || !snap.TriageEnabled {
		t.Fatalf("digest/triage 布尔应由 DB 补缺：%+v", &snap)
	}
	if snap.DigestEnabledSource != "database" || snap.TriageEnabledSource != "database" {
		t.Fatalf("digest/triage 来源应为 database：%+v", &snap)
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

// Retries 来源判定：默认 1 视为 unset（管理台可编辑），显式偏离默认（含 0）标记 env。
func TestRuntimeRetriesSource(t *testing.T) {
	// 默认配置加载：Retries 注入默认 1，来源 unset。
	cfg, err := config.Load(t.Context(), config.LoadOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}
	snap := RuntimeFromEnv(cfg.AI).Snapshot()
	if snap.Retries != 1 {
		t.Fatalf("默认重试应为 1，实际 %d", snap.Retries)
	}
	if snap.RetriesSource != "unset" {
		t.Fatalf("默认重试来源应 unset（管理台可编辑），实际 %s", snap.RetriesSource)
	}

	// env 显式设置 3 → env 锁定。
	env3 := RuntimeFromEnv(config.AIConfig{Retries: 3})
	if env3.Snapshot().RetriesSource != "env" {
		t.Fatalf("env 显式 3 应标记 env，实际 %s", env3.Snapshot().RetriesSource)
	}

	// env 显式设置 0（不重试）同样是偏离默认 → env 锁定。
	env0 := RuntimeFromEnv(config.AIConfig{Retries: 0})
	if env0.Snapshot().RetriesSource != "env" {
		t.Fatalf("env 显式 0 应标记 env，实际 %s", env0.Snapshot().RetriesSource)
	}
}

// TestLoadStoredConfig损坏即报错 守护：库内 AI 配置 JSON 损坏时返回错误，
// 而非静默返回零值（否则「已配置的 AI 突然全没了」且无任何日志）。
func TestLoadStoredConfig损坏即报错(t *testing.T) {
	data := openRuntimeStore(t)
	// 注意：存储层拒绝语法非法的 JSON，损坏语义用「类型不匹配」表达（数组无法解码为配置结构）。
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: RuntimeSettingKey,
		ValueJSON: json.RawMessage(`["corrupt"]`), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStoredConfig(t.Context(), data); err == nil {
		t.Fatal("损坏的 AI 配置应报错")
	}
}

// TestMergeFromStore解密失败即报错 守护：API Key 信封无法解密时返回错误，
// 而非静默降级为「未配置」（AI 全部不工作但配置页显示已配置）。
func TestMergeFromStore解密失败即报错(t *testing.T) {
	data := openRuntimeStore(t)
	// 用另一把密钥加密的信封：当前 keyRing 解不开。
	otherKey := make([]byte, 32)
	otherKey[0] = 0xff
	otherRing, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(otherKey)),
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := EncryptAPIKey(t.Context(), &otherRing, "sk-other")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveStoredConfig(t.Context(), data, StoredConfig{APIKeyEnvelope: env}); err != nil {
		t.Fatal(err)
	}
	rt := RuntimeFromEnv(config.AIConfig{})
	if err := MergeFromStore(t.Context(), data, testKeyRing(t), rt); err == nil {
		t.Fatal("信封无法用当前密钥解密时应报错")
	}
}
