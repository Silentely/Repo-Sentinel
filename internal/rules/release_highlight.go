package rules

import (
	"strings"
)

// ReleaseHighlights 包含分类提取后的版本更新亮点。
type ReleaseHighlights struct {
	Features []string `json:"features"`
	Fixes    []string `json:"fixes"`
	Breaking []string `json:"breaking"`
}

// ExtractReleaseHighlights 从发布日志原文中智能提取新功能、缺陷修复与破坏性变更。
func ExtractReleaseHighlights(notes string) ReleaseHighlights {
	lines := strings.Split(notes, "\n")
	var h ReleaseHighlights

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)

		switch {
		case strings.Contains(lower, "breaking") || strings.Contains(lower, "破坏性"):
			if len(h.Breaking) < 3 {
				h.Breaking = append(h.Breaking, trimmed)
			}
		case strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "修复"):
			if len(h.Fixes) < 5 {
				h.Fixes = append(h.Fixes, trimmed)
			}
		case strings.Contains(lower, "feat") || strings.Contains(lower, "feature") || strings.Contains(lower, "新增"):
			if len(h.Features) < 5 {
				h.Features = append(h.Features, trimmed)
			}
		}
	}

	return h
}
