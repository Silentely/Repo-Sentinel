package notify

import (
	"strings"
	"testing"
)

func TestNormalizeMarkdownQuotes(t *testing.T) {
	input := "Header paragraph\n>First quote line\n>   Second quote line\n>>>Third deeply nested line"
	normalized := NormalizeMarkdownQuotes(input)

	// 必须在 Header paragraph 和引用块之间插入空行
	expectedHeaderBreak := "Header paragraph\n\n> First quote line"
	if !strings.Contains(normalized, expectedHeaderBreak) {
		t.Errorf("expected header break, got:\n%s", normalized)
	}

	// 验证第二行空格标准化
	if !strings.Contains(normalized, "> Second quote line") {
		t.Errorf("expected standard space after quote, got:\n%s", normalized)
	}

	// 验证深层嵌套扁平化为 2 层
	if !strings.Contains(normalized, "> > Third deeply nested line") {
		t.Errorf("expected flattened 2-level quote, got:\n%s", normalized)
	}
}

func TestNormalizeMarkdownQuotes_Empty(t *testing.T) {
	if NormalizeMarkdownQuotes("") != "" {
		t.Errorf("expected empty string")
	}
}
