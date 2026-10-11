package notify

import (
	"strings"
	"testing"
)

func TestHandleTelegramCallback(t *testing.T) {
	// 1. 空 query
	if res := HandleTelegramCallback(nil); res.CallbackQueryID != "" {
		t.Fatalf("nil query 应返回空应答")
	}

	// 2. ack 回执动作
	ackQuery := &TelegramCallbackQuery{
		ID:   "qid_1",
		Data: "ack:outbox_12345",
	}
	ackRes := HandleTelegramCallback(ackQuery)
	if ackRes.CallbackQueryID != "qid_1" {
		t.Errorf("expected qid_1, got %s", ackRes.CallbackQueryID)
	}
	if !strings.Contains(ackRes.Text, "告警通知已确认") {
		t.Errorf("ack 动作应返回确认文本，实际: %s", ackRes.Text)
	}
	if ackRes.ShowAlert {
		t.Errorf("ack 动作用 toast 即可，不应弹窗 alert")
	}

	// 3. rerun 重试动作（重要操作，弹窗 Alert 强提醒）
	rerunQuery := &TelegramCallbackQuery{
		ID:   "qid_2",
		Data: "rerun:token_xyz",
	}
	rerunRes := HandleTelegramCallback(rerunQuery)
	if !strings.Contains(rerunRes.Text, "工作流重试指令") {
		t.Errorf("rerun 动作应返回重试文本，实际: %s", rerunRes.Text)
	}
	if !rerunRes.ShowAlert {
		t.Errorf("rerun 应开启 ShowAlert 弹出对话框确认")
	}

	// 4. mute 静音动作
	muteQuery := &TelegramCallbackQuery{
		ID:   "qid_3",
		Data: "mute:owner/repo:1h",
	}
	muteRes := HandleTelegramCallback(muteQuery)
	if !strings.Contains(muteRes.Text, "免打扰") {
		t.Errorf("mute 动作应返回免打扰提示，实际: %s", muteRes.Text)
	}
}
