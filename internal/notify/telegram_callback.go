package notify

import (
	"strings"
)

// TelegramUpdate 表示 Telegram Webhook 推送的 Update 对象片段。
type TelegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query,omitempty"`
}

// TelegramCallbackQuery 表示用户点击内联键盘按钮产生的数据。
type TelegramCallbackQuery struct {
	ID      string `json:"id"`
	From    struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	} `json:"from"`
	Data    string `json:"data"`
}

// TelegramAnswerCallbackQueryPayload 回应 Telegram callback_query 的应答体。
type TelegramAnswerCallbackQueryPayload struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

// HandleTelegramCallback 处理 Telegram 内联按钮点击事件，生成人类友好的即时交互反馈。
func HandleTelegramCallback(query *TelegramCallbackQuery) TelegramAnswerCallbackQueryPayload {
	if query == nil || query.ID == "" {
		return TelegramAnswerCallbackQueryPayload{}
	}

	data := strings.TrimSpace(query.Data)
	switch {
	case strings.HasPrefix(data, "ack:"):
		return TelegramAnswerCallbackQueryPayload{
			CallbackQueryID: query.ID,
			Text:            "✅ 告警通知已确认，标记为已知悉！",
			ShowAlert:       false,
		}
	case strings.HasPrefix(data, "rerun:"):
		return TelegramAnswerCallbackQueryPayload{
			CallbackQueryID: query.ID,
			Text:            "🔄 工作流重试指令已发出，正在通知 GitHub Actions 执行...",
			ShowAlert:       true,
		}
	case strings.HasPrefix(data, "mute:"):
		return TelegramAnswerCallbackQueryPayload{
			CallbackQueryID: query.ID,
			Text:            "🔕 已为您设置该仓库的临时免打扰！",
			ShowAlert:       false,
		}
	default:
		return TelegramAnswerCallbackQueryPayload{
			CallbackQueryID: query.ID,
			Text:            "👌 操作已收到！",
			ShowAlert:       false,
		}
	}
}
