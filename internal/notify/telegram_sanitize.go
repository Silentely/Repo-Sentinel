package notify

import (
	"regexp"
	"strings"
)

var (
	reBRTag        = regexp.MustCompile(`(?i)<br\s*/?>`)
	reFullHTMLTag  = regexp.MustCompile(`(?i)</?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?>`)
	reAllowedTag   = regexp.MustCompile(`(?i)^</?(?:b|strong|i|em|u|ins|s|strike|del|code|pre|tg-spoiler|blockquote)(?:\s+expandable)?\s*>$|^<a\s+href="[^"]*"\s*>$|^</a>$`)
	reBareLessThan = regexp.MustCompile(`(^|[^<])<([^a-zA-Z/])`)
)

// SanitizeTelegramHTML 清洗待推送到 Telegram 的 HTML 内容：
// 1. 将 <br> 和 <br/> 替换为换行符 \n；
// 2. 将非法的裸 < （如 "x < 5"）转义为 &lt;，避免解析器报错；
// 3. 过滤剥离不在 Telegram 官方支持白名单内的 HTML 标签（如 <div>, <p>, <span>, <script> 等）；
// 4. 保留 Telegram 支持的 <b>, <i>, <code>, <pre>, <a>, <blockquote> 及其属性。
func SanitizeTelegramHTML(input string) string {
	if input == "" {
		return ""
	}

	// 1. 规范化换行标签
	res := reBRTag.ReplaceAllString(input, "\n")

	// 2. 裸小于号转义（非标签开头的 <）
	if len(res) >= 2 && res[0] == '<' && (res[1] < 'A' || res[1] > 'Z') && (res[1] < 'a' || res[1] > 'z') && res[1] != '/' {
		res = "&lt;" + res[1:]
	}
	res = reBareLessThan.ReplaceAllString(res, "$1&lt;$2")

	// 3. 标签白名单过滤：非白名单标签仅保留其内部内容
	res = reFullHTMLTag.ReplaceAllStringFunc(res, func(tag string) string {
		if reAllowedTag.MatchString(tag) {
			return tag
		}
		// 特殊处理 <blockquote expandable>
		lower := strings.ToLower(tag)
		if strings.HasPrefix(lower, "<blockquote") && strings.Contains(lower, "expandable") {
			return "<blockquote expandable>"
		}
		// 剥离非法标签
		return ""
	})

	return res
}
