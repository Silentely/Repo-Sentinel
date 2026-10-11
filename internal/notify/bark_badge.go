package notify

// AssignBarkSoundAndLevel 按事件严重度给出 Bark 的提示音与打扰档位。
// critical 档穿透专注与静音，需要 Bark 客户端具备相应权限；high 档使用 timeSensitive，
// 仅在专注模式白名单内提示；medium/low 保持常规响铃与静默入历史。
func AssignBarkSoundAndLevel(severity string) (sound string, level string) {
	switch severity {
	case "critical":
		return "alarm", "critical"
	case "high":
		return "bell", "timeSensitive"
	case "low":
		return "", "passive"
	default:
		return "", "active"
	}
}
