package notify

import (
	"strings"
)

// GetTelegramEventBadge 根据 GitHub 事件类型、操作与状态智能推导最贴合的 Telegram 状态徽标
func GetTelegramEventBadge(eventType, action, status string) string {
	evt := strings.ToLower(strings.TrimSpace(eventType))
	act := strings.ToLower(strings.TrimSpace(action))
	stat := strings.ToLower(strings.TrimSpace(status))

	switch evt {
	case "pull_request":
		switch {
		case act == "closed" && stat == "merged":
			return "🟣" // 已合并
		case act == "closed":
			return "⚪" // 已关闭未合并
		case act == "opened" || act == "reopened":
			return "🟢" // 开启中
		default:
			return "🔄"
		}

	case "workflow_run", "check_run":
		switch stat {
		case "success":
			return "🟢"
		case "failure", "timed_out":
			return "🔴"
		case "in_progress", "queued":
			return "⏳"
		case "cancelled":
			return "⚪"
		default:
			return "⚙️"
		}

	case "issues":
		switch act {
		case "opened":
			return "🟡"
		case "closed":
			return "🟣"
		default:
			return "📋"
		}

	case "release":
		return "🎉"

	case "security_advisory", "dependabot_alert", "code_scanning_alert":
		switch stat {
		case "critical":
			return "🚨"
		case "high":
			return "🛡️"
		default:
			return "⚠️"
		}

	case "push":
		return "🚀"

	default:
		return "🔔"
	}
}
