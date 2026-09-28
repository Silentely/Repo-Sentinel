// Package textutil 提供跨包复用的文本处理工具。
package textutil

import (
	"strings"
	"unicode/utf8"
)

// MaxBodyTextBytes 为 Issue/PR 正文的统一长度上限，同时约束两处：
// 归一化写入事件载荷的正文副本，以及送入 LLM 的正文。
// 两处必须引用同一常量：落库上限低于 LLM 上限时，模型只会静默拿到被截断的副本，
// 连「正文已截断」的提示都触发不了。
const MaxBodyTextBytes = 6000

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
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
