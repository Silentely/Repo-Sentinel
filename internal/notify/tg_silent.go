package notify

import (
	"strings"
)

// ShouldDisableNotification 根据事件严重度与业务时段智能判断是否开启 Telegram 静默免打扰投递
func ShouldDisableNotification(severity, eventType string, isQuietHours bool) bool {
	sev := strings.ToLower(strings.TrimSpace(severity))

	// 极高危严重告警无论何时均强制响铃强提醒
	if sev == "critical" || sev == "fatal" {
		return false
	}

	// 处于日落休息免打扰时段，除极高危外均转为静默发送
	if isQuietHours {
		return true
	}

	// 纯信息同步类事件默认静默发送，降低打扰
	evt := strings.ToLower(strings.TrimSpace(eventType))
	if sev == "info" || sev == "low" || evt == "ping" || evt == "health_check" {
		return true
	}

	return false
}

// BuildTelegramSendParams 构建符合 Telegram Bot API 规范的发送参数映射
func BuildTelegramSendParams(chatID int64, text, parseMode string, disableNotification bool) map[string]interface{} {
	if parseMode == "" {
		parseMode = "HTML"
	}
	return map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"parse_mode":           parseMode,
		"disable_notification": disableNotification,
	}
}
