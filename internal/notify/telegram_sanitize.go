package notify

import (
	"regexp"
	"strings"
)

var (
	reBRTag = regexp.MustCompile(`(?i)<br\s*/?>`)
	// reAllowedTag 只放行 Telegram 官方支持的标签；code/pre 允许携带 class 等属性，
	// blockquote 允许 expandable 修饰，其余标签一律剥离但保留内部文本。
	reAllowedTag = regexp.MustCompile(
		`(?i)^</?(?:b|strong|i|em|u|ins|s|strike|del|tg-spoiler)\s*>$|` +
			`^<(?:code|pre)(?:\s+[^>]*)?>$|^</(?:code|pre)>$|` +
			`^<blockquote(?:\s+expandable)?\s*>$|^</blockquote>$|` +
			`^<a\s+href="[^"]*"\s*>$|^</a>$`)
	// reTagLike 匹配「完整标签形状」的片段（允许连字符/下划线标签名与属性），其余裸 < 一律转义。
	reTagLike = regexp.MustCompile(`(?s)</?[a-zA-Z][a-zA-Z0-9_-]*(?:\s[^>]*)?>`)
	// namedEntities 为 Telegram 官方支持的非数字实体：其余 &word; 形如未知实体，
	// 直接放行会让 Telegram 解析失败，故按其规则转义。
	namedEntities = map[string]bool{"amp": true, "lt": true, "gt": true, "quot": true}
)

// SanitizeTelegramHTML 清洗待推送到 Telegram 的 HTML 内容：
// 1. 将 <br> 和 <br/> 替换为换行符 \n；
// 2. 将未构成实体的裸 & 转义为 &amp;（仓库名或标题含 "R&D" 时否则会触发 400 解析失败）；
// 3. 只保留白名单标签形状的尖括号片段，其余裸 < 转义为 &lt;（含 "a<<b" 这类连写）；
// 4. 剥离 <div>、<p>、<span>、<script> 等非白名单标签，保留其内部文本。
func SanitizeTelegramHTML(input string) string {
	if input == "" {
		return ""
	}

	res := reBRTag.ReplaceAllString(input, "\n")
	res = escapeBareAmpersands(res)

	var out strings.Builder
	out.Grow(len(res) + 8)
	for {
		idx := strings.IndexByte(res, '<')
		if idx < 0 {
			out.WriteString(res)
			break
		}
		out.WriteString(res[:idx])
		rest := res[idx:]

		if loc := reTagLike.FindStringIndex(rest); loc != nil && loc[0] == 0 {
			tag := rest[:loc[1]]
			switch {
			case reAllowedTag.MatchString(tag):
				out.WriteString(tag)
			case strings.HasPrefix(strings.ToLower(tag), "<blockquote") && strings.Contains(strings.ToLower(tag), "expandable"):
				out.WriteString("<blockquote expandable>")
			}
			// 非白名单标签整体剥离，内部文本在后续循环中保留。
			res = rest[loc[1]:]
			continue
		}

		out.WriteString("&lt;")
		res = rest[1:]
	}
	return out.String()
}

// escapeBareAmpersands 把未构成 HTML 实体的 & 转义为 &amp;，已是实体形态的保持原样。
// Go 正则不支持负向前瞻，故用一次扫描判断实体边界。
func escapeBareAmpersands(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	var out strings.Builder
	out.Grow(len(text) + 8)
	for i := 0; i < len(text); {
		if text[i] != '&' {
			out.WriteByte(text[i])
			i++
			continue
		}
		if n, ok := htmlEntityLen(text[i:]); ok {
			out.WriteString(text[i : i+n])
			i += n
			continue
		}
		out.WriteString("&amp;")
		i++
	}
	return out.String()
}

// htmlEntityLen 判定以 & 开头的片段是否为 Telegram 可解析的 HTML 实体，返回其字节长度。
// Telegram 只支持 &amp; &lt; &gt; &quot; 四个命名实体与数字实体（&#39; / &#x27;），
// 其他 &word; 形式的未知实体一律转义，避免解析失败。
func htmlEntityLen(s string) (int, bool) {
	if len(s) < 3 || s[0] != '&' {
		return 0, false
	}
	i := 1
	if s[i] == '#' {
		i++
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i++
			start := i
			for i < len(s) && isHexDigit(s[i]) {
				i++
			}
			if i == start || i-start > 6 {
				return 0, false
			}
		} else {
			start := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i == start || i-start > 7 {
				return 0, false
			}
		}
	} else {
		start := i
		for i < len(s) && (isASCIILetter(s[i]) || (i > start && s[i] >= '0' && s[i] <= '9')) {
			i++
		}
		if i == start || !namedEntities[strings.ToLower(s[start:i])] {
			return 0, false
		}
	}
	if i < len(s) && s[i] == ';' {
		return i + 1, true
	}
	return 0, false
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
