package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
)

// 进程内计数器（单实例 Prometheus 文本暴露；多副本各自独立）。
var (
	metricWebhookAccepted   atomic.Uint64
	metricWebhookDuplicate  atomic.Uint64
	metricWebhookInvalidSig atomic.Uint64
	metricWebhookFailed     atomic.Uint64
	metricOutboxSent        atomic.Uint64
	metricOutboxDead        atomic.Uint64
	metricReconcileRuns     atomic.Uint64
)

// MetricsIncWebhookAccepted 记录 Webhook 接受。
func MetricsIncWebhookAccepted() { metricWebhookAccepted.Add(1) }

// MetricsIncWebhookDuplicate 记录重复 Delivery。
func MetricsIncWebhookDuplicate() { metricWebhookDuplicate.Add(1) }

// MetricsIncWebhookInvalidSig 记录验签失败。
func MetricsIncWebhookInvalidSig() { metricWebhookInvalidSig.Add(1) }

// MetricsIncWebhookFailed 记录后台处理失败（规范化或规则评估），与 WebhookDelivery 的 failed 状态对应。
func MetricsIncWebhookFailed() { metricWebhookFailed.Add(1) }

// MetricsIncOutboxSent 记录通知发送成功。
func MetricsIncOutboxSent() { metricOutboxSent.Add(1) }

// MetricsIncOutboxDead 记录通知进入死信。
func MetricsIncOutboxDead() { metricOutboxDead.Add(1) }

// MetricsIncReconcileRuns 记录对账执行次数。
func MetricsIncReconcileRuns() { metricReconcileRuns.Add(1) }

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// 可选：配置了独立 Token 时要求 Bearer；未配置则仅建议内网访问（文档说明）。
	if token := strings.TrimSpace(s.dependencies.Config.Metrics.Token.Reveal()); token != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
			return
		}
	}

	var b strings.Builder
	writeMetric := func(name, help, typ string, value uint64) {
		b.WriteString("# HELP ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(help)
		b.WriteString("\n# TYPE ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(typ)
		b.WriteString("\n")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(fmt.Sprintf("%d", value))
		b.WriteString("\n")
	}

	writeMetric("reposentinel_webhook_accepted_total", "Accepted GitHub webhook deliveries", "counter", metricWebhookAccepted.Load())
	writeMetric("reposentinel_webhook_duplicate_total", "Duplicate GitHub webhook deliveries", "counter", metricWebhookDuplicate.Load())
	writeMetric("reposentinel_webhook_invalid_signature_total", "Webhook signature failures", "counter", metricWebhookInvalidSig.Load())
	writeMetric("reposentinel_webhook_failed_total", "Webhook deliveries that failed processing", "counter", metricWebhookFailed.Load())
	writeMetric("reposentinel_outbox_sent_total", "Successfully delivered notifications", "counter", metricOutboxSent.Load())
	writeMetric("reposentinel_outbox_dead_total", "Notifications moved to dead letter", "counter", metricOutboxDead.Load())
	writeMetric("reposentinel_reconcile_runs_total", "Reconcile job executions", "counter", metricReconcileRuns.Load())

	// AI 调用指标：成功率/延迟/成本可观测（与日志同源，出口统一计数）。
	aiRequests, aiFailures, aiDurMS, aiPromptTok, aiCompTok, aiFailByCode := ai.MetricsSnapshot()
	writeMetric("reposentinel_ai_requests_total", "LLM calls issued (success + failure)", "counter", aiRequests)
	writeMetric("reposentinel_ai_requests_failed_total", "Failed LLM calls", "counter", aiFailures)
	if aiRequests > 0 {
		writeMetric("reposentinel_ai_request_duration_avg_ms", "Average LLM call duration in ms", "gauge", aiDurMS/aiRequests)
	}
	writeMetric("reposentinel_ai_prompt_tokens_total", "Prompt tokens consumed", "counter", aiPromptTok)
	writeMetric("reposentinel_ai_completion_tokens_total", "Completion tokens consumed", "counter", aiCompTok)
	for _, code := range ai.SortedFailCodes(aiFailByCode) {
		writeMetric("reposentinel_ai_requests_failed_"+code+"_total", "Failed LLM calls by error code", "counter", aiFailByCode[code])
	}

	// 实时库内仪表（若 Store 可用）
	if s.dependencies.Store != nil {
		if stats, err := s.dependencies.Store.Dashboard(r.Context()); err == nil {
			writeMetric("reposentinel_open_issues", "Open issues currently stored", "gauge", uint64(stats.OpenIssues))
			writeMetric("reposentinel_open_pulls", "Open pull requests currently stored", "gauge", uint64(stats.OpenPulls))
			writeMetric("reposentinel_failed_actions", "Failed workflow runs currently stored", "gauge", uint64(stats.FailedActions))
			writeMetric("reposentinel_open_security_alerts", "Open security alerts currently stored", "gauge", uint64(stats.OpenSecurity))
			writeMetric("reposentinel_outbox_dead_gauge", "Current dead-letter outbox count", "gauge", uint64(stats.OutboxDead))
			writeMetric("reposentinel_repos_active", "Active repositories", "gauge", uint64(stats.ReposActive))
			writeMetric("reposentinel_repos_baseline", "Repositories in baseline sync", "gauge", uint64(stats.ReposBaseline))
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
