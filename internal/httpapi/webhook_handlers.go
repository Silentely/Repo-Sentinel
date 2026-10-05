package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const maxWebhookBody = 1 << 20 // 1 MiB

// webhookNotConfiguredRetryAfter 未配置 Webhook Secret 时 503 响应的 Retry-After 秒数：
// GitHub 对 5xx 按此窗口退避重试，避免高频重试打满日志与入库。
const webhookNotConfiguredRetryAfter = "60"

func (s *server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeAPIError(w, r, http.StatusRequestEntityTooLarge, errorCodeValidationFailed, nil)
		return
	}
	// 提前读取投递标识：拒绝路径（未配置/签名失败）也需记录 delivery/event，
	// 便于按投递 ID 在 GitHub 侧与本地日志间交叉定位审计线索。
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))

	var secrets []string
	if s.dependencies.GitHubRuntime != nil {
		secrets = s.dependencies.GitHubRuntime.WebhookSecrets()
	} else {
		secrets = make([]string, 0, 2)
		if v := s.dependencies.Config.GitHub.WebhookSecret.Reveal(); v != "" {
			secrets = append(secrets, v)
		}
		if v := s.dependencies.Config.GitHub.WebhookPreviousSecret.Reveal(); v != "" {
			secrets = append(secrets, v)
		}
	}
	if len(secrets) == 0 {
		s.dependencies.Logger.Warn(
			"github webhook rejected",
			"request_id", requestIDFromContext(r.Context()),
			"delivery_id", deliveryID,
			"event_type", eventType,
			"error_code", errorCodeWebhookNotConfigured,
		)
		// GitHub 对 5xx 会按退避重试：给出明确窗口，避免配置未就绪期间高频重试打满日志与入库。
		// webhookNotConfiguredRetryAfter 与 errors.go 的登录限流窗口同语义（秒）。
		w.Header().Set("Retry-After", webhookNotConfiguredRetryAfter)
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeWebhookNotConfigured, nil)
		return
	}
	if !githubx.VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), secrets...) {
		MetricsIncWebhookInvalidSig()
		s.dependencies.Logger.Warn(
			"github webhook rejected",
			"request_id", requestIDFromContext(r.Context()),
			"delivery_id", deliveryID,
			"event_type", eventType,
			"error_code", errorCodeInvalidSignature,
		)
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidSignature, nil)
		return
	}

	if deliveryID == "" || eventType == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	if existing, err := s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), deliveryID); err == nil {
		s.respondWebhookDuplicate(w, r, &existing, deliveryID, eventType)
		return
	}

	delivery, err := s.dependencies.Store.WebhookDeliveries().Create(r.Context(), store.WebhookDelivery{
		ID: ulid.Make().String(), DeliveryID: deliveryID, EventType: eventType,
		Status: store.DeliveryAccepted, Payload: body, ReceivedAt: time.Now().UTC(),
	})
	if err == nil {
		s.broadcastDeliveryStage(DeliveryStageAccepted, deliveryID, 0, "webhook accepted", nil)
	}
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 冲突说明行已存在：取回行状态，accepted 卡死时走重放路径。
			existing, gerr := s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), deliveryID)
			if gerr != nil {
				s.respondWebhookDuplicate(w, r, nil, deliveryID, eventType)
			} else {
				s.respondWebhookDuplicate(w, r, &existing, deliveryID, eventType)
			}
			return
		}
		s.writeMappedError(w, r, err)
		return
	}

	var claimToken string
	if s.dependencies.Background == nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Error("webhook delivery cannot be scheduled: background context missing", "delivery_id", deliveryID, "error_code", "webhook_schedule_unavailable")
		}
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	token, ok, claimErr := s.dependencies.Store.WebhookDeliveries().ClaimWebhookForProcessing(r.Context(), delivery.ID, s.getWorkerID(), 3*time.Minute)
	if claimErr != nil || !ok {
		if s.dependencies.Logger != nil {
			attrs := []any{"delivery_id", deliveryID, "error_code", "webhook_claim_unavailable"}
			if claimErr != nil {
				attrs = append(attrs, "error", claimErr.Error())
			}
			s.dependencies.Logger.Error("failed to claim webhook delivery", attrs...)
		}
		// 保留 accepted 行，让 GitHub 的 5xx 重试或 orphan recovery 接管；不能伪造 202。
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	claimToken = token

	// 尽快 202，后台规范化（带并发限流，见 processWebhookAsync）。
	MetricsIncWebhookAccepted()
	s.dependencies.Logger.Info(
		"github webhook accepted",
		"delivery_id", deliveryID,
		"event_type", eventType,
		"payload_bytes", len(body),
	)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":      "accepted",
		"delivery_id": deliveryID,
	})

	s.processWebhookAsync(delivery.ID, eventType, deliveryID, claimToken, body)
}

// respondWebhookDuplicate 处理重复投递：GitHub 可能重发同一 delivery_id。
// 已处理的行幂等应答；仍停留在 accepted 的行（进程在入库与处理之间崩溃/关闭，
// 或实例关闭期间后台未消费）用行内载荷重放一次：事件级指纹幂等兜底，
// 重复处理无害，但能避免「载荷已入库却从未处理」的事件静默丢失。
func (s *server) respondWebhookDuplicate(w http.ResponseWriter, r *http.Request, existing *store.WebhookDelivery, deliveryID, eventType string) {
	MetricsIncWebhookDuplicate()
	status := ""
	if existing != nil {
		status = existing.Status
	}
	s.dependencies.Logger.Info(
		"github webhook duplicate",
		"delivery_id", deliveryID,
		"event_type", eventType,
		"row_status", status,
	)
	recoverable := existing != nil && existing.Status == store.DeliveryAccepted
	oldProcessing := existing != nil && existing.Status == store.DeliveryProcessing && time.Since(existing.ReceivedAt) > 2*time.Minute
	if recoverable || oldProcessing {
		if s.dependencies.Background == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
			return
		}
		// accepted 行可能刚经历 claim 失败，重复投递必须立即重试 CAS claim；processing 行只恢复超时租约。
		claimToken, ok, err := s.dependencies.Store.WebhookDeliveries().ClaimWebhookForProcessing(context.Background(), existing.ID, s.getWorkerID(), 3*time.Minute)
		if err != nil || !ok {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("failed to recover duplicate webhook delivery", "delivery_id", deliveryID, "error_code", "webhook_claim_unavailable", "error", err)
			}
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
			return
		}
		s.dependencies.Logger.Warn("webhook accepted row replayed",
			"delivery_id", deliveryID, "event_type", existing.EventType,
			"error_code", "accepted_replay", "age_ms", time.Since(existing.ReceivedAt).Milliseconds())
		s.processWebhookAsync(existing.ID, existing.EventType, existing.DeliveryID, claimToken, existing.Payload)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":      "duplicate",
		"delivery_id": deliveryID,
	})
}
