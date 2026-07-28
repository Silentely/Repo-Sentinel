package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestMetricsEndpointOptionalToken(t *testing.T) {
	s := &server{
		dependencies: Dependencies{
			Config: config.Config{
				Metrics: config.MetricsConfig{Enabled: true},
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "reposentinel_webhook_accepted_total") {
		t.Fatalf("body=%s", rec.Body.String())
	}

	s.dependencies.Config.Metrics.Token = config.NewSecret("tok")
	rec2 := httptest.NewRecorder()
	s.handleMetrics(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec2.Code)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req2.Header.Set("Authorization", "Bearer tok")
	rec3 := httptest.NewRecorder()
	s.handleMetrics(rec3, req2)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rec3.Code)
	}
}
