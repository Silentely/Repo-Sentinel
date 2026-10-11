package notify

import (
	"testing"
)

func TestHotReloadEngine(t *testing.T) {
	engine := NewHotReloadEngine()

	// 初始修订版本为 0
	if engine.Revision() != 0 {
		t.Errorf("expected initial revision 0, got %d", engine.Revision())
	}

	// 尝试应用非法空配置
	_, err := engine.ApplyUpdate(ChannelConfig{Channel: "", Enabled: true})
	if err == nil {
		t.Fatalf("expected error for empty channel name")
	}

	// 应用合法配置
	cfgTelegram := ChannelConfig{
		Channel:  "telegram",
		Token:    "bot123456:ABC-DEF",
		Endpoint: "https://api.telegram.org",
		Enabled:  true,
	}

	changed, err := engine.ApplyUpdate(cfgTelegram)
	if err != nil || !changed {
		t.Fatalf("expected successful update, got changed=%v, err=%v", changed, err)
	}
	if engine.Revision() != 1 {
		t.Errorf("expected revision 1, got %d", engine.Revision())
	}

	// 读取配置
	fetched, found := engine.GetConfig("TELEGRAM")
	if !found || fetched.Token != "bot123456:ABC-DEF" {
		t.Errorf("failed to get config case-insensitively: %+v", fetched)
	}

	// 再次应用相同配置，changed 应为 false，且 revision 不递增
	changedSame, _ := engine.ApplyUpdate(cfgTelegram)
	if changedSame {
		t.Errorf("expected changed=false for identical config")
	}
	if engine.Revision() != 1 {
		t.Errorf("expected revision unchanged at 1, got %d", engine.Revision())
	}
}
