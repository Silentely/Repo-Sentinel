package httpapi

import (
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
			"error_code", "webhook_not_configured",
		)
		// GitHub 对 5xx 会按退避重试：给出明确窗口，避免配置未就绪期间高频重试打满日志与入库。
		w.Header().Set("Retry-After", "60")
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "webhook_not_configured", nil)
		return
	}
	if !githubx.VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), secrets...) {
		MetricsIncWebhookInvalidSig()
		s.dependencies.Logger.Warn(
			"github webhook rejected",
			"request_id", requestIDFromContext(r.Context()),
			"delivery_id", deliveryID,
			"event_type", eventType,
			"error_code", "invalid_signature",
		)
		s.writeAPIError(w, r, http.StatusUnauthorized, "invalid_signature", nil)
		return
	}

	if deliveryID == "" || eventType == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	if existing, err := s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), deliveryID); err == nil {
		s.respondWebhookDuplicate(w, existing.DeliveryID, eventType)
		return
	}

	delivery, err := s.dependencies.Store.WebhookDeliveries().Create(r.Context(), store.WebhookDelivery{
		ID: ulid.Make().String(), DeliveryID: deliveryID, EventType: eventType,
		Status: store.DeliveryAccepted, Payload: body, ReceivedAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.respondWebhookDuplicate(w, deliveryID, eventType)
			return
		}
		s.writeMappedError(w, r, err)
		return
	}

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

	s.processWebhookAsync(delivery.ID, eventType, deliveryID, body)
}

// respondWebhookDuplicate 处理重复投递：GitHub 可能重发同一 delivery_id，
// 记录指标与日志后以 202 duplicate 幂等应答，不重复入库与处理。
func (s *server) respondWebhookDuplicate(w http.ResponseWriter, deliveryID, eventType string) {
	MetricsIncWebhookDuplicate()
	s.dependencies.Logger.Info(
		"github webhook duplicate",
		"delivery_id", deliveryID,
		"event_type", eventType,
	)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":      "duplicate",
		"delivery_id": deliveryID,
	})
}
