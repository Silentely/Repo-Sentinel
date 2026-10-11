package notify

// BarkNotificationOptions 描述 Bark 推送的高级配置项
type BarkNotificationOptions struct {
	Badge     int    `json:"badge"`      // 角标数字，设为 0 可清除
	Sound     string `json:"sound"`      // 提醒声音名称
	Group     string `json:"group"`      // 通知分组
	IsArchive int    `json:"isArchive"`  // 1: 保存至历史, 0: 不保存
	Level     string `json:"level"`      // active, timeSensitive, passive
}

// AssignBarkSoundAndLevel 根据事件严重度自动匹配最贴合的 iOS 声音与打扰级别
func AssignBarkSoundAndLevel(severity string) (sound string, level string) {
	switch severity {
	case "critical":
		return "alarm.caf", "timeSensitive"
	case "high":
		return "bell.caf", "active"
	case "medium":
		return "glass.caf", "active"
	default:
		return "silence.caf", "passive"
	}
}

// BuildBarkPayloadWithExt 构建带角标和声音扩展的 Bark 推送 Payload 字典
func BuildBarkPayloadWithExt(title, body, group string, opts BarkNotificationOptions) map[string]interface{} {
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
		"group": group,
	}

	if opts.Badge >= 0 {
		payload["badge"] = opts.Badge
	}
	if opts.Sound != "" {
		payload["sound"] = opts.Sound
	}
	if opts.Level != "" {
		payload["level"] = opts.Level
	}
	if opts.IsArchive == 0 || opts.IsArchive == 1 {
		payload["isArchive"] = opts.IsArchive
	}

	return payload
}
