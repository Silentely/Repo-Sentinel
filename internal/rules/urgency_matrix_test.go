package rules

import (
	"strings"
	"testing"
)

func TestDetermineVulnerabilityUrgency(t *testing.T) {
	// 1. P0 顶格
	p0 := DetermineVulnerabilityUrgency("critical", 9.8, true)
	if p0.Priority != "P0" || !strings.Contains(p0.ActionLabel, "顶格致命") {
		t.Fatalf("CVSS 9.8 + remote 应为 P0，got: %+v", p0)
	}

	// 2. P1 高危
	p1 := DetermineVulnerabilityUrgency("high", 7.5, false)
	if p1.Priority != "P1" {
		t.Fatalf("high 应为 P1，got: %+v", p1)
	}

	// 3. P3 低危
	p3 := DetermineVulnerabilityUrgency("low", 2.1, false)
	if p3.Priority != "P3" {
		t.Fatalf("low 应为 P3，got: %+v", p3)
	}
}
