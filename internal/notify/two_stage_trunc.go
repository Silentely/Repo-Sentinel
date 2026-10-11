package notify

import (
	"unicode/utf8"
)

// TruncateWithPreservedFooter 进行两阶段截断：优先裁剪中间正文，坚决保全底部关键操作链接与页脚。
func TruncateWithPreservedFooter(mainText, footer string, maxLimit int) string {
	if maxLimit <= 0 {
		maxLimit = 4000
	}

	mainLen := utf8.RuneCountInString(mainText)
	footerLen := utf8.RuneCountInString(footer)

	if mainLen+footerLen <= maxLimit {
		if footer == "" {
			return mainText
		}
		return mainText + "\n" + footer
	}

	availableForMain := maxLimit - footerLen - 4 // 预留 "\n…\n" 空间
	if availableForMain <= 0 {
		return truncateRunes(mainText+footer, maxLimit)
	}

	truncatedMain := truncateRunes(mainText, availableForMain) + "…"
	if footer == "" {
		return truncatedMain
	}
	return truncatedMain + "\n" + footer
}
