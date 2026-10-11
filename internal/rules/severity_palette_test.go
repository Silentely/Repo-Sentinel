package rules

import (
	"testing"
)

func TestResolveSeverityPalette(t *testing.T) {
	crit := ResolveSeverityPalette(SeverityCritical)
	if crit.BadgeEmoji != "🔴" || crit.FeishuTemplate != "red" || crit.SlackHex != "#E01E5A" {
		t.Errorf("unexpected critical palette: %+v", crit)
	}

	high := ResolveSeverityPalette(SeverityHigh)
	if high.BadgeEmoji != "🟠" || high.FeishuTemplate != "orange" {
		t.Errorf("unexpected high palette: %+v", high)
	}

	unknown := ResolveSeverityPalette("non_existent")
	if unknown.Level != SeverityInfo || unknown.BadgeEmoji != "⚪" {
		t.Errorf("unexpected fallback palette: %+v", unknown)
	}
}
