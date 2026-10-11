package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluateSystemDiagnostics_AllHealthy(t *testing.T) {
	comps := []ComponentDiagnostic{
		{Name: "telegram", Status: StatusHealthy},
		{Name: "store", Status: StatusHealthy},
	}
	report := EvaluateSystemDiagnostics(comps)
	if report.OverallStatus != StatusHealthy {
		t.Errorf("expected HEALTHY, got %s", report.OverallStatus)
	}
}

func TestEvaluateSystemDiagnostics_DegradedAndUnhealthy(t *testing.T) {
	compsDegraded := []ComponentDiagnostic{
		{Name: "telegram", Status: StatusHealthy},
		{Name: "wecom", Status: StatusDegraded, Message: "Rate limiting observed"},
	}
	repDegraded := EvaluateSystemDiagnostics(compsDegraded)
	if repDegraded.OverallStatus != StatusDegraded {
		t.Errorf("expected DEGRADED, got %s", repDegraded.OverallStatus)
	}

	compsUnhealthy := []ComponentDiagnostic{
		{Name: "store", Status: StatusUnhealthy, Message: "Database disconnected"},
		{Name: "telegram", Status: StatusHealthy},
	}
	repUnhealthy := EvaluateSystemDiagnostics(compsUnhealthy)
	if repUnhealthy.OverallStatus != StatusUnhealthy {
		t.Errorf("expected UNHEALTHY, got %s", repUnhealthy.OverallStatus)
	}
}

func TestDiagnosticsHandler(t *testing.T) {
	handler := DiagnosticsHandler(func() []ComponentDiagnostic {
		return []ComponentDiagnostic{
			{Name: "telegram", Status: StatusHealthy},
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/system/diagnostics", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp SystemDiagnosticsReport
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.OverallStatus != StatusHealthy {
		t.Errorf("expected overall healthy, got %s", resp.OverallStatus)
	}
}
