package rules

import (
	"strings"
	"testing"
)

func TestClassifySecurityThreat_ExactCWE(t *testing.T) {
	threat := ClassifySecurityThreat("CWE-89")
	if threat.CWEID != "CWE-89" {
		t.Errorf("expected CWE-89, got %s", threat.CWEID)
	}
	if !strings.Contains(threat.OWASPCategory, "A03") {
		t.Errorf("expected A03 category, got %s", threat.OWASPCategory)
	}
	if threat.SeverityTier != "🔴 CRITICAL" {
		t.Errorf("expected critical tier, got %s", threat.SeverityTier)
	}
}

func TestClassifySecurityThreat_KeywordFallback(t *testing.T) {
	threat := ClassifySecurityThreat("Potential hardcoded api key exposed in auth.py")
	if threat.CWEID != "CWE-798" {
		t.Errorf("expected CWE-798 for leaked api key, got %s", threat.CWEID)
	}

	summary := threat.FormatThreatCardSummary()
	if !strings.Contains(summary, "🔴 CRITICAL") || !strings.Contains(summary, "建议") {
		t.Errorf("unexpected card summary format: %s", summary)
	}
}

func TestClassifySecurityThreat_Unknown(t *testing.T) {
	threat := ClassifySecurityThreat("unknown generic issue")
	if threat.CWEID != "CWE-OTHER" {
		t.Errorf("expected CWE-OTHER, got %s", threat.CWEID)
	}
}
