package notify

import (
	"testing"
)

func TestEscapeTelegramMarkdownV2(t *testing.T) {
	raw := "Hello [World]! Version 1.0.0 (Release #1)"
	got := EscapeTelegramMarkdownV2(raw)
	expected := "Hello \\[World\\]\\! Version 1\\.0\\.0 \\(Release \\#1\\)"
	if got != expected {
		t.Fatalf("expected: %s, got: %s", expected, got)
	}
}
