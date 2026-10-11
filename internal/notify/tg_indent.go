package notify

import (
	"fmt"
	"strings"
)

// FormatIndentedBlock 为 Telegram 富文本生成结构化紧凑对齐的缩进文本块
// title: 块标题 (加粗)
// lines: 列表条目
// maxIndent: 最大空格缩进层数，防止窄屏下文本折叠断行
func FormatIndentedBlock(title string, lines []string, maxIndent int) string {
	if len(lines) == 0 {
		if title == "" {
			return ""
		}
		return fmt.Sprintf("<b>%s</b>", EscapeHTML(title))
	}

	if maxIndent <= 0 {
		maxIndent = 4
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString("<b>")
		sb.WriteString(EscapeHTML(title))
		sb.WriteString("</b>\n")
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// 计算前导空格，限制不超过 maxIndent
		leadingSpaces := 2
		if leadingSpaces > maxIndent {
			leadingSpaces = maxIndent
		}

		indent := strings.Repeat(" ", leadingSpaces)
		// 如果不是列表符号开头，补充圆点符号
		if !strings.HasPrefix(trimmed, "•") && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") {
			sb.WriteString(fmt.Sprintf("%s• %s", indent, EscapeHTML(trimmed)))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s", indent, EscapeHTML(trimmed)))
		}

		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
