package notify

import (
	"fmt"
	"strings"
)

// FormatMarkdownTableAsFeishuFields 将多列二维表格转译为飞书卡片窄屏友好对齐的列表
func FormatMarkdownTableAsFeishuFields(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, row := range rows {
		var parts []string
		for colIdx, cell := range row {
			cellVal := strings.TrimSpace(cell)
			if cellVal == "" {
				cellVal = "-"
			}
			if colIdx < len(headers) && headers[colIdx] != "" {
				parts = append(parts, fmt.Sprintf("**%s**: %s", headers[colIdx], cellVal))
			} else {
				parts = append(parts, cellVal)
			}
		}

		line := "• " + strings.Join(parts, " | ")
		sb.WriteString(line)
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
