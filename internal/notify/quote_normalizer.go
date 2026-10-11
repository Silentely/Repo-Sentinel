package notify

import (
	"strings"
)

// NormalizeMarkdownQuotes 对飞书与钉钉 Markdown 中的引用块语法进行标准化修剪
func NormalizeMarkdownQuotes(input string) string {
	if input == "" {
		return ""
	}

	lines := strings.Split(input, "\n")
	var result []string
	prevIsNormalText := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, ">") {
			// 如果前一行是非空的普通文本，插入空行隔离，避免解析器粘连
			if prevIsNormalText {
				result = append(result, "")
			}

			// 统计前导 > 的层级
			quoteLevel := 0
			idx := 0
			for idx < len(trimmed) && trimmed[idx] == '>' {
				quoteLevel++
				idx++
			}

			// 限制最多 2 层嵌套，多余的扁平化
			if quoteLevel > 2 {
				quoteLevel = 2
			}

			content := strings.TrimSpace(trimmed[idx:])
			var normalizedLine string
			if content == "" {
				// 空引用行
				normalizedLine = strings.Repeat(">", quoteLevel)
			} else {
				prefix := ""
				for i := 0; i < quoteLevel; i++ {
					prefix += "> "
				}
				normalizedLine = prefix + content
			}

			result = append(result, normalizedLine)
			prevIsNormalText = false
		} else {
			result = append(result, line)
			if trimmed != "" {
				prevIsNormalText = true
			} else {
				prevIsNormalText = false
			}
		}
	}

	return strings.Join(result, "\n")
}
