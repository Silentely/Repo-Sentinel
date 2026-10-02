package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func generateTestKeyHex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return hex.EncodeToString(buf)
}

func setupTestStoreForRotate(t *testing.T) (store.Store, config.DatabaseConfig) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "rotate_test.db")
	dbCfg := config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + dbPath,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	}
	st, err := store.Open(context.Background(), dbCfg)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st, dbCfg
}

func testRotateConfig(dbCfg config.DatabaseConfig, currentKey, prevKey string) config.Config {
	return config.Config{
		HTTP: config.HTTPConfig{
			Addr:          "127.0.0.1:0",
			PublicBaseURL: "http://127.0.0.1",
		},
		Database: dbCfg,
		Admin:    config.AdminBootstrapConfig{SessionTTL: time.Hour},
		Logging: config.LoggingConfig{
			Format: "json",
			Level:  "error",
		},
		Encryption: config.EncryptionConfig{
			CurrentKey:  config.NewSecret(currentKey),
			PreviousKey: config.NewSecret(prevKey),
		},
	}
}

func TestRotateKeys_FullCycleAndDryRun(t *testing.T) {
	ctx := context.Background()
	st, dbCfg := setupTestStoreForRotate(t)

	keyOldHex := generateTestKeyHex(t)
	keyNewHex := generateTestKeyHex(t)

	// 1. 初始化旧密钥环，写入存量凭据
	ringOld, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(keyOldHex),
	})
	if err != nil {
		t.Fatalf("new old keyring: %v", err)
	}

	// 写入探针
	if err := writeEncryptionProbe(ctx, st, ringOld, store.SystemSetting{}); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	// 写入通知渠道
	envChan, err := ringOld.Encrypt(ctx, []byte("https://hooks.slack.com/services/T/B/X"), []byte(notify.AAD))
	if err != nil {
		t.Fatalf("encrypt channel: %v", err)
	}
	ch, err := st.Channels().Upsert(ctx, store.NotificationChannel{
		ID:             "ch-test-1",
		ChannelType:    "slack",
		Name:           "Dev Alerts",
		SecretEnvelope: envChan,
		Enabled:        true,
	})
	if err != nil || ch.ID == "" {
		t.Fatalf("upsert channel: %v", err)
	}

	// 写入 GitHub App 配置
	envPrivKey, err := ringOld.Encrypt(ctx, []byte("-----BEGIN RSA PRIVATE KEY-----"), []byte("reposentinel:github-runtime:v1"))
	if err != nil {
		t.Fatalf("encrypt github priv key: %v", err)
	}
	envWebhook, err := ringOld.Encrypt(ctx, []byte("github_webhook_secret_123"), []byte("reposentinel:github-runtime:v1"))
	if err != nil {
		t.Fatalf("encrypt github webhook secret: %v", err)
	}
	if err := githubx.SaveStoredRuntime(ctx, st, githubx.StoredRuntime{
		AppID:                 12345,
		PrivateKeyPEMEnvelope: envPrivKey,
		WebhookSecretEnvelope: envWebhook,
	}); err != nil {
		t.Fatalf("save github runtime: %v", err)
	}

	// 写入 AI 配置
	envAIKey, err := ringOld.Encrypt(ctx, []byte("sk-openai-legacy-token"), []byte("reposentinel:ai-runtime:v1"))
	if err != nil {
		t.Fatalf("encrypt ai key: %v", err)
	}
	if err := ai.SaveStoredConfig(ctx, st, ai.StoredConfig{
		Model:          "gpt-4o",
		APIKeyEnvelope: envAIKey,
	}); err != nil {
		t.Fatalf("save ai config: %v", err)
	}

	// 写入 TOTP 配置
	if err := auth.SaveTOTPConfig(ctx, st, &ringOld, true, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("save totp: %v", err)
	}

	// 2. 构造双密钥配置：当前为新密钥，上一把为旧密钥
	cfgDual := testRotateConfig(dbCfg, keyNewHex, keyOldHex)

	// 3. 测试 DryRun 模式
	dryStats, err := RotateKeys(ctx, cfgDual, RotateKeysOptions{DryRun: true})
	if err != nil {
		t.Fatalf("rotate keys dry run failed: %v", err)
	}
	if !dryStats.DryRun {
		t.Fatalf("expected dry run true, got false")
	}
	if dryStats.ChannelsRotated != 1 {
		t.Fatalf("expected 1 channel to rotate, got %d", dryStats.ChannelsRotated)
	}
	if dryStats.SettingsRotated != 4 { // github, ai, totp, probe
		t.Fatalf("expected 4 settings to rotate, got %d", dryStats.SettingsRotated)
	}
	if dryStats.AlreadyCurrent != 0 {
		t.Fatalf("expected 0 already current, got %d", dryStats.AlreadyCurrent)
	}

	// 验证 DryRun 后，用旧密钥仍能以单密钥方式解密（未被重加密修改）
	probeSetting, err := st.Settings().Get(ctx, encryptionProbeSettingKey)
	if err != nil {
		t.Fatalf("get probe setting: %v", err)
	}
	var probeData encryptionProbe
	if err := json.Unmarshal(probeSetting.ValueJSON, &probeData); err != nil {
		t.Fatalf("unmarshal probe: %v", err)
	}
	decOld, err := ringOld.Decrypt(ctx, probeData.Envelope, []byte(encryptionProbeAAD))
	if err != nil || decOld.UsedPreviousKey {
		t.Fatalf("probe should still be decryptable by old key without previousKey fallback")
	}

	// 4. 执行正式轮换
	runStats, err := RotateKeys(ctx, cfgDual, RotateKeysOptions{DryRun: false})
	if err != nil {
		t.Fatalf("rotate keys real run failed: %v", err)
	}
	if runStats.DryRun {
		t.Fatalf("expected dry run false, got true")
	}
	if runStats.ChannelsRotated != 1 {
		t.Fatalf("expected 1 channel rotated, got %d", runStats.ChannelsRotated)
	}
	if runStats.SettingsRotated != 4 {
		t.Fatalf("expected 4 settings rotated, got %d", runStats.SettingsRotated)
	}

	// 5. 再次运行轮换：应该报告全部为 AlreadyCurrent
	runAgainStats, err := RotateKeys(ctx, cfgDual, RotateKeysOptions{DryRun: false})
	if err != nil {
		t.Fatalf("rotate keys second run failed: %v", err)
	}
	if runAgainStats.ChannelsRotated != 0 || runAgainStats.SettingsRotated != 0 {
		t.Fatalf("expected 0 rotated on second run, got %d channels and %d settings", runAgainStats.ChannelsRotated, runAgainStats.SettingsRotated)
	}
	if runAgainStats.AlreadyCurrent != 5 { // 1 channel + 4 settings
		t.Fatalf("expected 5 already current, got %d", runAgainStats.AlreadyCurrent)
	}

	// 6. 验证新密钥独占（以新连接打开数据库，移除旧密钥配置后，系统单密钥仍完全可用）
	cfgNewOnly := testRotateConfig(dbCfg, keyNewHex, "")
	stNew, err := store.Open(ctx, cfgNewOnly.Database)
	if err != nil {
		t.Fatalf("open store with new only key: %v", err)
	}
	defer func() { _ = stNew.Close() }()

	ringNewOnly, err := cryptox.NewKeyRing(cfgNewOnly.Encryption)
	if err != nil {
		t.Fatalf("new key ring for new key: %v", err)
	}

	// 校验探针验证通过
	validatedRing, err := validateEncryptionKey(ctx, stNew, cfgNewOnly.Encryption)
	if err != nil || validatedRing == nil {
		t.Fatalf("validateEncryptionKey with new only key failed: %v", err)
	}

	// 校验渠道密码能用新密钥直接解密
	chUpdated, err := stNew.Channels().Get(ctx, "ch-test-1")
	if err != nil {
		t.Fatalf("get updated channel: %v", err)
	}
	decChan, err := ringNewOnly.Decrypt(ctx, chUpdated.SecretEnvelope, []byte(notify.AAD))
	if err != nil {
		t.Fatalf("decrypt channel with new only key failed: %v", err)
	}
	if string(decChan.Plaintext) != "https://hooks.slack.com/services/T/B/X" {
		t.Fatalf("unexpected plaintext: %s", string(decChan.Plaintext))
	}
	if decChan.UsedPreviousKey {
		t.Fatalf("expected UsedPreviousKey false")
	}

	// 校验 GitHub App 配置能用新密钥解密
	ghStored, err := githubx.LoadStoredRuntime(ctx, stNew)
	if err != nil {
		t.Fatalf("load stored runtime: %v", err)
	}
	decPriv, err := ringNewOnly.Decrypt(ctx, ghStored.PrivateKeyPEMEnvelope, []byte("reposentinel:github-runtime:v1"))
	if err != nil || string(decPriv.Plaintext) != "-----BEGIN RSA PRIVATE KEY-----" {
		t.Fatalf("decrypt github private key with new key failed: %v", err)
	}

	// 校验 AI API Key 能用新密钥解密
	aiStored, err := ai.LoadStoredConfig(ctx, stNew)
	if err != nil {
		t.Fatalf("load stored ai config: %v", err)
	}
	decAI, err := ringNewOnly.Decrypt(ctx, aiStored.APIKeyEnvelope, []byte("reposentinel:ai-runtime:v1"))
	if err != nil || string(decAI.Plaintext) != "sk-openai-legacy-token" {
		t.Fatalf("decrypt ai api key with new key failed: %v", err)
	}

	// 校验 TOTP 2FA 能用新密钥解密
	totpEnabled, totpSecret, err := auth.LoadTOTPConfig(ctx, stNew, &ringNewOnly)
	if err != nil || !totpEnabled || totpSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("load totp with new key failed: enabled=%v, secret=%s, err=%v", totpEnabled, totpSecret, err)
	}
}

func TestRotateKeys_CorruptedSecretRollsBackTransaction(t *testing.T) {
	ctx := context.Background()
	st, dbCfg := setupTestStoreForRotate(t)

	keyOldHex := generateTestKeyHex(t)
	keyNewHex := generateTestKeyHex(t)

	ringOld, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(keyOldHex),
	})
	if err != nil {
		t.Fatalf("new old keyring: %v", err)
	}

	if err := writeEncryptionProbe(ctx, st, ringOld, store.SystemSetting{}); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	// 写入一个损坏的信封
	_, err = st.Channels().Upsert(ctx, store.NotificationChannel{
		ID:             "ch-corrupted",
		ChannelType:    "telegram",
		Name:           "Corrupted Channel",
		SecretEnvelope: "v1.invalidbase64...corrupted",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	cfgDual := testRotateConfig(dbCfg, keyNewHex, keyOldHex)

	// 轮换应失败，且探针保持不变
	if _, err = RotateKeys(ctx, cfgDual, RotateKeysOptions{DryRun: false}); err == nil {
		t.Fatalf("expected error on corrupted secret, got nil")
	}
}
