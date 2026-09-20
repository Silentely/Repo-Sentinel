package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	// 处理失败计数应始终有行（含 0 值），便于监控端预置告警。
	if !strings.Contains(body, "reposentinel_webhook_failed_total") {
		t.Fatalf("期望包含 webhook 处理失败指标行，body=%s", body)
	}
	// 指标计数器累加后可观测到增量。
	before := metricWebhookFailed.Load()
	MetricsIncWebhookFailed()
	if after := metricWebhookFailed.Load(); after != before+1 {
		t.Fatalf("失败计数应 +1，before=%d after=%d", before, after)
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

// TestMetricsEndpointExposesOutboxQueueDepth 指标端点应暴露待投递/发送中队列深度：
// 投递积压可监控（行始终存在，含 0 值）。
func TestMetricsEndpointExposesOutboxQueueDepth(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{metricsEnabled: true})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "reposentinel_outbox_pending_gauge") ||
		!strings.Contains(body, "reposentinel_outbox_sending_gauge") {
		t.Fatalf("期望包含 outbox 队列深度指标行，body=%s", body)
	}
}

// TestShouldLogMetricsAccess 守护 /metrics 访问日志的按 IP 采样：
// 同一 IP 在采样窗口内只记一条、窗口外放行、达到跟踪上限后整体重置。
func TestShouldLogMetricsAccess(t *testing.T) {
	// 包级采样表跨用例共享，先清空保证时序断言不受其他用例影响。
	metricsLogMu.Lock()
	metricsLogLastSeen = make(map[string]time.Time)
	metricsLogMu.Unlock()

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	if !shouldLogMetricsAccess("203.0.113.7", now) {
		t.Fatal("首次访问应记录")
	}
	if shouldLogMetricsAccess("203.0.113.7", now.Add(5*time.Second)) {
		t.Fatal("同一 IP 采样窗口内不应重复记录")
	}
	if !shouldLogMetricsAccess("203.0.113.7", now.Add(11*time.Second)) {
		t.Fatal("超出采样窗口后应放行记录")
	}
	if !shouldLogMetricsAccess("203.0.113.8", now) {
		t.Fatal("不同 IP 不受其他 IP 的采样状态影响")
	}

	// 达到跟踪上限后表整体清空：新 IP 放行，且旧 IP 的窗口记录一并作废（重新计时）。
	for i := 0; i < metricsLogMaxTrackedIPs; i++ {
		shouldLogMetricsAccess(fmt.Sprintf("198.51.100.%d", i), now.Add(30*time.Second))
	}
	if !shouldLogMetricsAccess("198.51.100.254", now.Add(30*time.Second)) {
		t.Fatal("超出跟踪上限后新 IP 应放行")
	}
	if !shouldLogMetricsAccess("203.0.113.7", now.Add(31*time.Second)) {
		t.Fatal("表重置后旧 IP 应重新开始计时")
	}
}
