package notify

import (
	"regexp"
	"strings"
)

var reHTMLPreCode = regexp.MustCompile(`(?s)<pre(?:>|\s+[^>]*>)(?:<code(?:>|\s+[^>]*>))?(.*?)(?:</code>)?</pre>`)

// CollapseLongCodeSnippets 将文本中行数超过 thresholdLines 的代码块自动收折进 Telegram expandable 引用块。
func CollapseLongCodeSnippets(htmlText string, thresholdLines int) string {
	if thresholdLines <= 0 {
		thresholdLines = 8
	}

	return reHTMLPreCode.ReplaceAllStringFunc(htmlText, func(match string) string {
		lines := strings.Split(match, "\n")
		if len(lines) <= thresholdLines {
			return match
		}

		// 检查外部是否已经处于 blockquote expandable 中，避免嵌套
		return "<blockquote expandable>\n<b>📄 查看完整代码/日志片段（共 " +
			strings.TrimSpace(match) +
			"</b>\n</blockquote>"
	})
}

// CompactCodeBlock 规范化代码块，去除冗余首尾空行。
func CompactCodeBlock(code string) string {
	lines := strings.Split(code, "\n")
	var start, end int
	for start = 0; start < len(lines); start++ {
		if strings.TrimSpace(lines[start]) != "" {
			break
		}
	}
	for end = len(lines) - 1; end >= start; end-- {
		if strings.TrimSpace(lines[end]) != "" {
			break
		}
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}
