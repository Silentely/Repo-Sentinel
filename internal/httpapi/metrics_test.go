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
	body := rec.Body.String()
	if !strings.Contains(body, "reposentinel_webhook_accepted_total") {
		t.Fatalf("body=%s", body)
	}
	// AI 指标默认输出（计数为 0 也应有行，便于监控端预置告警）。
	if !strings.Contains(body, "reposentinel_ai_requests_total") ||
		!strings.Contains(body, "reposentinel_ai_prompt_tokens_total") {
		t.Fatalf("期望包含 AI 指标行，body=%s", body)
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
