package notify

import (
	"testing"
)

func TestShouldDisableNotification(t *testing.T) {
	// 极高危告警在免打扰时段也不静默
	if ShouldDisableNotification("critical", "error", true) {
		t.Errorf("critical alert should never be silent even in quiet hours")
	}

	// 普通事件在免打扰时段应静默
	if !ShouldDisableNotification("medium", "push", true) {
		t.Errorf("medium push in quiet hours should be silent")
	}

	// info 等级非免打扰时段也默认静默
	if !ShouldDisableNotification("info", "ping", false) {
		t.Errorf("info ping should be silent by default")
	}

	// high 等级非免打扰时段应响铃
	if ShouldDisableNotification("high", "workflow_run", false) {
		t.Errorf("high workflow_run in working hours should not be silent")
	}
}

func TestBuildTelegramSendParams(t *testing.T) {
	params := BuildTelegramSendParams(1234567, "Hello <b>World</b>", "HTML", true)
	if params["chat_id"] != int64(1234567) || params["disable_notification"] != true {
		t.Errorf("unexpected params: %+v", params)
	}
}
