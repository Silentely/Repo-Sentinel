package rules

import (
	"strings"
	"testing"
)

func TestIsProductionBranch(t *testing.T) {
	prodBranches := []string{"main", "master", "prod", "production", "release/v1.0", "release-2.0"}
	for _, b := range prodBranches {
		if !IsProductionBranch(b) {
			t.Errorf("expected %s to be production branch", b)
		}
	}

	devBranches := []string{"feature/login", "fix/bug", "dependabot/sub", "test-1"}
	for _, b := range devBranches {
		if IsProductionBranch(b) {
			t.Errorf("expected %s to not be production branch", b)
		}
	}
}

func TestBranchBadge(t *testing.T) {
	if got := BranchBadge(""); got != "" {
		t.Errorf("empty branch should return empty badge")
	}

	mainBadge := BranchBadge("main")
	if !strings.Contains(mainBadge, "⭐") || !strings.Contains(mainBadge, "主干") {
		t.Errorf("main branch should have star and 主干 label, got: %s", mainBadge)
	}

	featBadge := BranchBadge("feature/login")
	if !strings.Contains(featBadge, "<code>feature/login</code>") {
		t.Errorf("feature branch should be code formatted, got: %s", featBadge)
	}
}
