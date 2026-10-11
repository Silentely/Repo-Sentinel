package notify

import (
	"fmt"
	"strings"
)

// WeComFontColor 企微支持的字体色彩类型
type WeComFontColor string

const (
	WeComColorInfo    WeComFontColor = "info"    // 绿色
	WeComColorComment WeComFontColor = "comment" // 灰色
	WeComColorWarning WeComFontColor = "warning" // 橙红色
)

// FormatWeComColoredText 包装企微专用颜色字体标签
func FormatWeComColoredText(text string, color WeComFontColor) string {
	if text == "" {
		return ""
	}
	if color == "" {
		color = WeComColorComment
	}
	return fmt.Sprintf("<font color=\"%s\">%s</font>", color, text)
}

// FormatWeComStatusBadge 将常见构建/执行状态转译为彩色徽标
func FormatWeComStatusBadge(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "success", "passed", "resolved":
		return FormatWeComColoredText("● 成功", WeComColorInfo)
	case "failure", "failed", "critical", "error":
		return FormatWeComColoredText("● 失败", WeComColorWarning)
	case "running", "in_progress", "pending":
		return FormatWeComColoredText("● 进行中", WeComColorComment)
	default:
		return FormatWeComColoredText("● "+status, WeComColorComment)
	}
}
