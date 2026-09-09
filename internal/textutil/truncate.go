// Package textutil 提供跨包复用的文本处理工具。
package textutil

import (
	"strings"
	"unicode/utf8"
)

// TruncateUTF8Bytes 将 value 截断到不超过 limit 字节且不破坏多字节 UTF-8 字符：
// 先清理非法字节序列，再回退到完整字符边界。limit <= 0 时返回空串。
func TruncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if len(value) <= limit {
		return value
	}
	truncated := value[:limit]
	for !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}
