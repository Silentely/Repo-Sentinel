package auth

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func openTOTPProbeStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "totp.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func probeKeyRing(t *testing.T) *cryptox.KeyRing {
	t.Helper()
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(strings.Repeat("ab", 32)),
	})
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	return &ring
}

// TestSaveTOTPConfigRejectsNilKeyRing 回归：密钥环不可用时启用 2FA 必须被拒绝，
// 不得把 TOTP 种子以明文写入 system_settings。原实现在 ring==nil 时写 plain_secret，
// 而 LoadTOTPConfig 又无条件接受该字段，使该状态可自洽运行（2FA 登录校验可用）——
// 取得数据库读权限者即可生成动态码，把第二因子降级为已知量。
func TestSaveTOTPConfigRejectsNilKeyRing(t *testing.T) {
	data := openTOTPProbeStore(t)
	const seed = "JBSWY3DPEHPK3PXP"

	if err := SaveTOTPConfig(t.Context(), data, nil, true, seed); err == nil {
		t.Fatal("密钥环不可用时启用 2FA 应被拒绝")
	}

	// 库内不得出现 plain_secret 明文字段。
	row, err := data.Settings().Get(t.Context(), TOTPSettingKey)
	if err == nil {
		if strings.Contains(string(row.ValueJSON), "plain_secret") {
			t.Fatalf("库内出现明文字段: %s", string(row.ValueJSON))
		}
		if strings.Contains(string(row.ValueJSON), seed) {
			t.Fatalf("库内出现明文种子: %s", string(row.ValueJSON))
		}
	}
	// 读取侧同样不得放行。
	if enabled, secret, err := LoadTOTPConfig(t.Context(), data, nil); err == nil && enabled {
		t.Fatalf("读取侧放行了无信封配置: enabled=%v secret=%q", enabled, secret)
	}
}

// TestSaveTOTPConfigEnvelopeRoundTrip 对照：装配密钥环时种子以 AES-GCM 信封落库，
// 且 LoadTOTPConfig 能解回同一值。
func TestSaveTOTPConfigEnvelopeRoundTrip(t *testing.T) {
	data := openTOTPProbeStore(t)
	const seed = "JBSWY3DPEHPK3PXP"
	ring := probeKeyRing(t)

	if err := SaveTOTPConfig(t.Context(), data, ring, true, seed); err != nil {
		t.Fatalf("save: %v", err)
	}
	row, err := data.Settings().Get(t.Context(), TOTPSettingKey)
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}
	if !strings.Contains(string(row.ValueJSON), "secret_envelope") {
		t.Fatalf("应写信封而非明文: %s", string(row.ValueJSON))
	}
	if strings.Contains(string(row.ValueJSON), "plain_secret") {
		t.Fatalf("不应出现 plain_secret: %s", string(row.ValueJSON))
	}
	enabled, got, err := LoadTOTPConfig(t.Context(), data, ring)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !enabled || got != seed {
		t.Fatalf("解回值不匹配: enabled=%v secret=%q", enabled, got)
	}
}

// TestLoadTOTPConfigRejectsLegacyPlainSecret 存量明文行（历史降级写入）必须 fail-closed：
// 不再被接受为可用配置。
func TestLoadTOTPConfigRejectsLegacyPlainSecret(t *testing.T) {
	data := openTOTPProbeStore(t)
	legacy := []byte(`{"enabled":true,"plain_secret":"JBSWY3DPEHPK3PXP","updated_at":"2026-01-01T00:00:00Z"}`)
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: "legacy-totp", Key: TOTPSettingKey, ValueJSON: legacy, UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	enabled, secret, err := LoadTOTPConfig(t.Context(), data, probeKeyRing(t))
	if err == nil {
		t.Fatalf("存量明文行应被拒绝，实际 enabled=%v secret=%q", enabled, secret)
	}
	if enabled || secret != "" {
		t.Fatalf("拒绝时不得返回可用值: enabled=%v secret=%q", enabled, secret)
	}
}

// TestDisableTOTPStillWorksWithoutKeyRing 停用 2FA 不涉及密钥材质，密钥环缺失时仍可执行。
func TestDisableTOTPStillWorksWithoutKeyRing(t *testing.T) {
	data := openTOTPProbeStore(t)
	if err := SaveTOTPConfig(t.Context(), data, probeKeyRing(t), true, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := DisableTOTP(t.Context(), data); err != nil {
		t.Fatalf("disable without key ring: %v", err)
	}
	enabled, _, err := LoadTOTPConfig(t.Context(), data, probeKeyRing(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if enabled {
		t.Fatal("停用后应报告未启用")
	}
}

var _ = context.Background
