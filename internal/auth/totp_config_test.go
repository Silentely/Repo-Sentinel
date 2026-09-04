package auth

import (
	"bytes"
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
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &ring
}

func testStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(dir, "totp_test.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestTOTPConfigStorageAndEncryption(t *testing.T) {
	st := testStore(t)
	ring := testKeyRing(t)
	ctx := t.Context()

	// 初始状态未启用
	enabled, secret, err := LoadTOTPConfig(ctx, st, ring)
	if err != nil {
		t.Fatalf("初始加载失败: %v", err)
	}
	if enabled || secret != "" {
		t.Fatalf("初始状态应未启用: enabled=%v, secret=%s", enabled, secret)
	}

	// 启用 2FA
	testSecret := "JBSWY3DPEHPK3PXP"
	if err := SaveTOTPConfig(ctx, st, ring, true, testSecret); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	enabled, secret, err = LoadTOTPConfig(ctx, st, ring)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	if !enabled || secret != testSecret {
		t.Fatalf("加载配置不匹配: enabled=%v, secret=%s", enabled, secret)
	}

	// 停用 2FA
	if err := DisableTOTP(ctx, st); err != nil {
		t.Fatalf("停用 2FA 失败: %v", err)
	}

	enabled, secret, err = LoadTOTPConfig(ctx, st, ring)
	if err != nil {
		t.Fatalf("停用后加载失败: %v", err)
	}
	if enabled || secret != "" {
		t.Fatalf("停用后状态应为 false: enabled=%v, secret=%s", enabled, secret)
	}
}

func TestTOTPTicketManager(t *testing.T) {
	mgr := NewTOTPTicketManager(3 * time.Minute)

	ticket := mgr.CreateTicket("admin-1", "admin", "192.168.1.100")
	if ticket == "" {
		t.Fatal("生成 ticket 为空")
	}

	// 错误 IP 拒绝
	info, ok := mgr.GetTicket(ticket, "192.168.1.101")
	if ok {
		t.Fatal("跨 IP 获取 ticket 应被拒绝")
	}

	// 正确获取
	info, ok = mgr.GetTicket(ticket, "192.168.1.100")
	if !ok || info.AdminID != "admin-1" || info.Username != "admin" {
		t.Fatalf("获取 ticket 失败: ok=%v, info=%+v", ok, info)
	}

	// 记录失败尝试
	if attempts := mgr.RecordFailure(ticket); attempts != 1 {
		t.Fatalf("失败次数=%d, want 1", attempts)
	}
	if attempts := mgr.RecordFailure(ticket); attempts != 2 {
		t.Fatalf("失败次数=%d, want 2", attempts)
	}
	if attempts := mgr.RecordFailure(ticket); attempts != 3 {
		t.Fatalf("失败次数=%d, want 3", attempts)
	}

	// 超过 3 次失败后 ticket 自动失效
	_, ok = mgr.GetTicket(ticket, "192.168.1.100")
	if ok {
		t.Fatal("连续 3 次失败后 ticket 应被销毁")
	}

	// 重新生成并单次消费销毁
	ticket2 := mgr.CreateTicket("admin-1", "admin", "192.168.1.100")
	mgr.ConsumeTicket(ticket2)
	_, ok = mgr.GetTicket(ticket2, "192.168.1.100")
	if ok {
		t.Fatal("已消费的 ticket 应被销毁")
	}
}
