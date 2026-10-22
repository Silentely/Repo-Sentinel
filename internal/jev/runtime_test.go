package jev

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestRuntimeEncryption(t *testing.T) {
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{CurrentKey: config.NewSecret(strings.Repeat("01", 32))})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := EncryptAPIKey(t.Context(), &ring, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(envelope, "fixture-key") {
		t.Fatal("明文泄露")
	}
	plain, err := DecryptAPIKey(t.Context(), &ring, envelope)
	if err != nil || plain != "fixture-key" {
		t.Fatalf("密文往返失败: %v", err)
	}
	if _, err := ring.Decrypt(t.Context(), envelope, []byte("reposentinel:ai-runtime:v1")); err == nil {
		t.Fatal("跨 AAD 解密必须失败")
	}

	// 边界条件：nil keyring 或空 key
	if enc, err := EncryptAPIKey(t.Context(), nil, "key"); err == nil || enc != "" {
		t.Error("nil keyring encrypt should fail")
	}
	if enc, err := EncryptAPIKey(t.Context(), &ring, ""); err != nil || enc != "" {
		t.Error("empty plain encrypt should return empty without error")
	}
	if dec, err := DecryptAPIKey(t.Context(), nil, "env"); err == nil || dec != "" {
		t.Error("nil keyring decrypt should fail")
	}
	if dec, err := DecryptAPIKey(t.Context(), &ring, ""); err != nil || dec != "" {
		t.Error("empty envelope decrypt should return empty without error")
	}
}

func TestRuntimeLifecycleAndClients(t *testing.T) {
	cfg := config.JevConfig{
		Enabled: true,
		BaseURL: "https://api.typesafe.ai",
		APIKey:  config.NewSecret("secret-key"),
		Model:   "jev-1.14.0",
		Mode:    "shadow",
		Notification: config.PlaneConfig{
			Timeout:        20 * time.Second,
			MaxConcurrency: 4,
		},
		Review: config.PlaneConfig{
			Timeout:        15 * time.Second,
			MaxConcurrency: 4,
		},
		ChatOps: config.PlaneConfig{
			Timeout:        10 * time.Second,
			MaxConcurrency: 2,
		},
	}

	rt := RuntimeFromEnv(cfg)
	if rt == nil {
		t.Fatal("RuntimeFromEnv returned nil")
	}

	// 1. 各决策面客户端正常构建
	notifCli := rt.Client("notification")
	if notifCli == nil {
		t.Fatal("notification client should not be nil")
	}
	if rt.Client("review") == nil {
		t.Fatal("review client should not be nil")
	}
	if rt.Client("chatops") == nil {
		t.Fatal("chatops client should not be nil")
	}

	// 2. 导出断路器与缓存状态
	bStats := rt.BreakerStats()
	if len(bStats) != 3 {
		t.Fatalf("expected 3 plane breaker stats, got %d", len(bStats))
	}
	if bStats["notification"].State != "closed" {
		t.Errorf("expected closed state, got %s", bStats["notification"].State)
	}

	cStats := rt.CacheStats()
	if len(cStats) != 3 {
		t.Fatalf("expected 3 plane cache stats, got %d", len(cStats))
	}

	// 3. 快照测试
	snap := rt.Snapshot()
	if snap.Model != "jev-1.14.0" || snap.APIKey.Reveal() != "secret-key" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// 4. SetClientForTest
	rt.SetClientForTest("custom_plane", notifCli)
	if rt.Client("custom_plane") == nil {
		t.Fatal("custom_plane should be set")
	}

	// 5. Replace
	cfg2 := cfg
	cfg2.Model = "jev-2.0.0"
	rt2 := RuntimeFromEnv(cfg2)
	rt.Replace(rt2)
	if rt.Snapshot().Model != "jev-2.0.0" {
		t.Fatalf("expected replaced model jev-2.0.0, got %s", rt.Snapshot().Model)
	}

	// Replace nil
	rt.Replace(nil)

	// Disabled runtime
	cfgDisabled := cfg
	cfgDisabled.Enabled = false
	rtDisabled := RuntimeFromEnv(cfgDisabled)
	if rtDisabled.Client("notification") != nil {
		t.Fatal("disabled runtime should have no clients")
	}
}

func TestRuntimeEmergencyAdvisory(t *testing.T) {
	cfg := config.JevConfig{
		Enabled: true,
		BaseURL: "https://api.typesafe.ai",
		Model:   "jev-1.14.0",
		Mode:    "active",
	}
	rt := RuntimeFromEnv(cfg)

	now := time.Now().UTC()
	until := now.Add(10 * time.Minute)
	reason := "upstream degradation"

	rt.SetEmergencyAdvisory(until, reason)
	gotUntil, gotReason := rt.EmergencyAdvisory()
	if !gotUntil.Equal(until) || gotReason != reason {
		t.Fatalf("unexpected emergency advisory: %v, %s", gotUntil, gotReason)
	}

	// Active 模式在降级截止时间内应判定为 Advisory
	if !rt.IsChatOpsAdvisory(now.Add(2 * time.Minute)) {
		t.Fatal("within advisory window should be advisory")
	}
	// 超出降级截止时间后不再是 Advisory
	if rt.IsChatOpsAdvisory(now.Add(15 * time.Minute)) {
		t.Fatal("after advisory window should not be advisory")
	}

	// Shadow 模式恒为 Advisory
	cfgShadow := cfg
	cfgShadow.Mode = "shadow"
	rtShadow := RuntimeFromEnv(cfgShadow)
	if !rtShadow.IsChatOpsAdvisory(now) {
		t.Fatal("shadow mode must be advisory")
	}
}

func TestRuntimeStorePersistenceAndMerge(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime.db")
	st, err := store.Open(ctx, config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + dbPath,
	})
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer st.Close()

	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{CurrentKey: config.NewSecret(strings.Repeat("02", 32))})
	if err != nil {
		t.Fatal(err)
	}

	// 初始为空
	loaded, err := LoadStoredConfig(ctx, st)
	if err != nil {
		t.Fatalf("load stored config failed: %v", err)
	}
	if loaded.Enabled != nil {
		t.Fatal("expected nil enabled on empty store")
	}

	// 保存配置
	enabled := true
	retries := 1
	envelope, _ := EncryptAPIKey(ctx, &ring, "db-secret-key")
	toSave := StoredConfig{
		Enabled:        &enabled,
		BaseURL:        "https://db.api.typesafe.ai",
		Model:          "jev-1.14.0",
		Mode:           "active",
		Retries:        &retries,
		APIKeyEnvelope: envelope,
		Notification: &config.PlaneConfig{
			Timeout:        100 * time.Millisecond, // 遗留小超时，MergeFromStore 会平滑提升至 30s
			MaxConcurrency: 6,
		},
	}
	if err := SaveStoredConfig(ctx, st, toSave); err != nil {
		t.Fatalf("save stored config failed: %v", err)
	}

	// 再次读取验证
	reloaded, err := LoadStoredConfig(ctx, st)
	if err != nil {
		t.Fatalf("reload stored config failed: %v", err)
	}
	if reloaded.BaseURL != "https://db.api.typesafe.ai" {
		t.Fatalf("unexpected reloaded base url: %s", reloaded.BaseURL)
	}

	// 运行时合并
	envCfg := config.JevConfig{
		Enabled: false,
		BaseURL: "https://env.api.typesafe.ai",
	}
	rt := RuntimeFromEnv(envCfg)
	if err := MergeFromStore(ctx, st, &ring, rt); err != nil {
		t.Fatalf("merge from store failed: %v", err)
	}

	merged := rt.Snapshot()
	if !merged.Enabled {
		t.Fatal("expected merged enabled=true")
	}
	if merged.APIKey.Reveal() != "db-secret-key" {
		t.Fatalf("expected decrypted db key, got %s", merged.APIKey.Reveal())
	}
	if merged.Notification.Timeout != 30*time.Second {
		t.Fatalf("expected timeout upgraded to 30s, got %v", merged.Notification.Timeout)
	}

	// 边界测试：nil store
	if err := MergeFromStore(ctx, nil, &ring, rt); err != nil {
		t.Errorf("merge with nil store should not error: %v", err)
	}
	if _, err := LoadStoredConfig(ctx, nil); err != nil {
		t.Errorf("load with nil store should not error: %v", err)
	}
	if err := SaveStoredConfig(ctx, nil, toSave); err != nil {
		t.Errorf("save with nil store should not error: %v", err)
	}
}
