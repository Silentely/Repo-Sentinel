package notify

import (
	"testing"
)

func TestAssignBarkSoundAndLevel(t *testing.T) {
	sndCrit, lvlCrit := AssignBarkSoundAndLevel("critical")
	if sndCrit != "alarm" || lvlCrit != "critical" {
		t.Errorf("unexpected critical bark sound/level: %s, %s", sndCrit, lvlCrit)
	}

	sndHigh, lvlHigh := AssignBarkSoundAndLevel("high")
	if sndHigh != "bell" || lvlHigh != "timeSensitive" {
		t.Errorf("unexpected high bark sound/level: %s, %s", sndHigh, lvlHigh)
	}

	sndMed, lvlMed := AssignBarkSoundAndLevel("medium")
	if sndMed != "" || lvlMed != "active" {
		t.Errorf("unexpected medium bark sound/level: %s, %s", sndMed, lvlMed)
	}

	// 低打扰档位用于依赖更新类推送：静默且不点亮屏幕。
	if sndLow, lvlLow := AssignBarkSoundAndLevel("low"); lvlLow != "passive" || sndLow != "" {
		t.Errorf("unexpected low bark sound/level: %s, %s", sndLow, lvlLow)
	}
}

// TestClassifySeverityUsesWordBoundary 覆盖子串误判回归：highlight 不应被当作 high 高危。
func TestClassifySeverityUsesWordBoundary(t *testing.T) {
	cases := []struct {
		title string
		body  string
		want  string
	}{
		{"highlight 变更说明", "", "medium"},
		{"Renovate 依赖更新：foo 1.2.3 → 1.2.4", "", "low"},
		{"⚠️ high severity 漏洞", "", "high"},
		{"❌ CI 构建失败", "", "critical"},
	}
	for _, tc := range cases {
		if got := classifySeverity(tc.title, tc.body); got != tc.want {
			t.Errorf("classifySeverity(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
