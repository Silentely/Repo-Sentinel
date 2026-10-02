package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
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
		!strings.Contains(body, "reposentinel_ai_prompt_tokens_total") ||
		!strings.Contains(body, "reposentinel_ai_tokens_total") ||
		!strings.Contains(body, "reposentinel_ai_cost_estimated_usd_total") {
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

// TestMetricsEndpointExposesOutboxQueueDepth 指标端点应暴露待传递/发送中队列深度：
// 传递积压可监控（行始终存在，含 0 值）。
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

// TestMetricsEndpointExposesSSEState 指标端点应暴露 SSE 在线订阅数与丢弃计数：
// 行始终存在（含 0 值），使实时推流健康可监控（丢弃 = 慢客户端背压）。
func TestMetricsEndpointExposesSSEState(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{metricsEnabled: true})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "reposentinel_sse_clients") ||
		!strings.Contains(body, "reposentinel_sse_dropped_total") {
		t.Fatalf("期望包含 SSE 指标行，body=%s", body)
	}
}

// TestMetricsFreshness 验证活跃仓库同步时效遥测与 Prometheus reposentinel_sync_max_lag_seconds 输出。
func TestMetricsFreshness(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{metricsEnabled: true})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	ctx := t.Context()
	now := time.Now().UTC()
	past400s := now.Add(-400 * time.Second)
	past100s := now.Add(-100 * time.Second)

	_, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-fresh-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "org", Name: "repo-1", FullName: "org/repo-1", LastSyncedAt: &past400s,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-fresh-2", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusBaseline,
		Owner: "org", Name: "repo-2", FullName: "org/repo-2", LastSyncedAt: &past100s,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. 指标端点输出校验
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "reposentinel_sync_max_lag_seconds") {
		t.Fatalf("expected reposentinel_sync_max_lag_seconds in metrics, body=%s", body)
	}

	// 2. Dashboard API freshness 结构解析校验
	resp := fixture.request(t, http.MethodGet, "/api/v1/dashboard", "", "127.0.0.1:45102", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	var stats store.DashboardStats
	if err := json.Unmarshal(resp.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Freshness == nil {
		t.Fatal("expected stats.Freshness to be non-nil")
	}
	if stats.Freshness.MaxLagSeconds < 400 {
		t.Fatalf("expected max_lag_seconds >= 400, got %d", stats.Freshness.MaxLagSeconds)
	}
	if stats.Freshness.LaggingRepoCount != 1 {
		t.Fatalf("expected lagging_repo_count == 1, got %d", stats.Freshness.LaggingRepoCount)
	}
	if stats.Freshness.MostLaggedRepoName != "org/repo-1" {
		t.Fatalf("expected most_lagged_repo_name 'org/repo-1', got %s", stats.Freshness.MostLaggedRepoName)
	}
	if stats.Freshness.HasSyncError {
		t.Fatalf("expected has_sync_error == false")
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
