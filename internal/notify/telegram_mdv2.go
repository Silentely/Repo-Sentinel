package notify

import (
	"strings"
)

var mdv2SpecialChars = []string{
	"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!",
}

// EscapeTelegramMarkdownV2 将文本中的 Telegram MarkdownV2 关键特殊符号安全转义。
func EscapeTelegramMarkdownV2(text string) string {
	res := text
	for _, ch := range mdv2SpecialChars {
		res = strings.ReplaceAll(res, ch, "\\"+ch)
	}
	return res
}
