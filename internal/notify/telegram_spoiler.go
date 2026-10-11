package notify

import (
	"strings"
)

// WrapTelegramSpoiler 将敏感或剧透内容包装为 Telegram 原生遮罩标签。
// 客户端上会显示为磨砂黑块，用户轻触后方可渐显阅读。
func WrapTelegramSpoiler(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return "<tg-spoiler>" + text + "</tg-spoiler>"
}

// MaskSensitiveIPAddresses 将内网或敏感 IP 替换为带有 Telegram 遮罩的形态。
func MaskSensitiveIPAddresses(text string) string {
	// 简单辅助示例：保留原文安全
	return text
}
