package notify

import (
	"fmt"
	"html"
	"strings"
)

// EscapeHTML escapes text inserted into Telegram HTML fragments.
func EscapeHTML(text string) string {
	return html.EscapeString(text)
}

// FormatTelegramDiffSnippet 将代码 Diff 补丁转换为 Telegram 原生着色代码块
func FormatTelegramDiffSnippet(diffText string, maxLines int) string {
	trimmed := strings.TrimSpace(diffText)
	if trimmed == "" {
		return ""
	}

	if maxLines <= 0 {
		maxLines = 20
	}

	lines := strings.Split(trimmed, "\n")
	var formattedLines []string

	omitted := 0
	if len(lines) > maxLines {
		omitted = len(lines) - maxLines
		lines = lines[:maxLines]
	}

	for _, line := range lines {
		// 转义 HTML 实体，避免破坏外层 pre 标签
		escaped := EscapeHTML(line)
		formattedLines = append(formattedLines, escaped)
	}

	if omitted > 0 {
		formattedLines = append(formattedLines, fmt.Sprintf("... (%d lines omitted)", omitted))
	}

	content := strings.Join(formattedLines, "\n")
	return fmt.Sprintf("<pre><code class=\"language-diff\">%s</code></pre>", content)
}
