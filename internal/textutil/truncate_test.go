package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8BytesASCIIBoundary(t *testing.T) {
	got := TruncateUTF8Bytes("abcdefghij", 5)
	if got != "abcde" {
		t.Fatalf("期望 abcde，got %q", got)
	}
}

func TestTruncateUTF8BytesKeepsRuneBoundary(t *testing.T) {
	// 截断点正好落在多字节字符中间（"测" 占 3 字节，limit 落在其后半段）。
	got := TruncateUTF8Bytes("a测b", 2)
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if got != "a" {
		t.Fatalf("期望回退到完整字符 a，got %q", got)
	}
}

func TestTruncateUTF8BytesShortInputUnchanged(t *testing.T) {
	value := "短文本"
	if got := TruncateUTF8Bytes(value, 100); got != value {
		t.Fatalf("短输入不应被改写，got %q", got)
	}
}

func TestTruncateUTF8BytesInvalidUTF8Cleaned(t *testing.T) {
	// 输入含非法字节（0xFF 不是合法 UTF-8 起始），清理后截断仍须合法。
	value := "ok" + string([]byte{0xFF, 0xFE}) + "中文"
	got := TruncateUTF8Bytes(value, 3)
	if !utf8.ValidString(got) {
		t.Fatalf("清理后的截断结果仍含非法字节: %q", got)
	}
	if got != "ok" {
		t.Fatalf("期望清理非法字节并截断为 ok，got %q", got)
	}
}

func TestTruncateUTF8BytesNonPositiveLimit(t *testing.T) {
	if got := TruncateUTF8Bytes("任何内容", 0); got != "" {
		t.Fatalf("limit=0 期望空串，got %q", got)
	}
	if got := TruncateUTF8Bytes("任何内容", -1); got != "" {
		t.Fatalf("limit<0 期望空串，got %q", got)
	}
}

func TestTruncateUTF8BytesLongMultibyteText(t *testing.T) {
	prefix := strings.Repeat("a", 7999)
	got := TruncateUTF8Bytes(prefix+"测试发布说明", 8000)
	if len(got) > 8000 {
		t.Fatalf("截断后长度 %d 超过上限", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8")
	}
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("截断结果丢失前缀")
	}
}
