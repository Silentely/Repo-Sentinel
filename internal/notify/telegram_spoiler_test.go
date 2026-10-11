package notify

import (
	"testing"
)

func TestWrapTelegramSpoiler(t *testing.T) {
	if got := WrapTelegramSpoiler(""); got != "" {
		t.Errorf("empty text should return empty, got %q", got)
	}

	raw := "secret-key-xyz"
	got := WrapTelegramSpoiler(raw)
	expected := "<tg-spoiler>secret-key-xyz</tg-spoiler>"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

	// 验证经过 SanitizeTelegramHTML 后不会被剔除
	sanitized := SanitizeTelegramHTML(got)
	if sanitized != got {
		t.Errorf("tg-spoiler should be preserved by SanitizeTelegramHTML, got %s", sanitized)
	}
}
