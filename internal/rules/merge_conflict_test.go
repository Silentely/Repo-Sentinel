package rules

import (
	"strings"
	"testing"
)

func TestInspectPRMergeability(t *testing.T) {
	// 场景 1: clean 就绪
	tr := true
	res1 := InspectPRMergeability(&tr, "clean", false)
	if !res1.CanAutoMerge || res1.ConflictDetected {
		t.Errorf("expected clean PR to be auto-mergeable: %+v", res1)
	}
	if !strings.Contains(res1.StatusBadge, "🟢") {
		t.Errorf("expected green badge, got %s", res1.StatusBadge)
	}

	// 场景 2: conflict 冲突
	fl := false
	res2 := InspectPRMergeability(&fl, "dirty", false)
	if res2.CanAutoMerge || !res2.ConflictDetected {
		t.Errorf("expected conflict detected: %+v", res2)
	}
	if !strings.Contains(res2.StatusBadge, "⚔️") {
		t.Errorf("expected swords badge, got %s", res2.StatusBadge)
	}

	// 场景 3: draft 草稿
	res3 := InspectPRMergeability(nil, "clean", true)
	if res3.CanAutoMerge || !strings.Contains(res3.StatusBadge, "草稿") {
		t.Errorf("expected draft status: %+v", res3)
	}
}
