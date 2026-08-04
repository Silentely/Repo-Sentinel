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
			"error_code", "webhook_not_configured",
		)
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "webhook_not_configured", nil)
		return
	}
	if !githubx.VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), secrets...) {
		MetricsIncWebhookInvalidSig()
		s.dependencies.Logger.Warn(
			"github webhook rejected",
			"request_id", requestIDFromContext(r.Context()),
			"error_code", "invalid_signature",
		)
		s.writeAPIError(w, r, http.StatusUnauthorized, "invalid_signature", nil)
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if deliveryID == "" || eventType == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	if existing, err := s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), deliveryID); err == nil {
		MetricsIncWebhookDuplicate()
		s.dependencies.Logger.Info(
			"github webhook duplicate",
			"delivery_id", existing.DeliveryID,
			"event_type", eventType,
		)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":      "duplicate",
			"delivery_id": existing.DeliveryID,
		})
		return
	}

	delivery, err := s.dependencies.Store.WebhookDeliveries().Create(r.Context(), store.WebhookDelivery{
		ID: ulid.Make().String(), DeliveryID: deliveryID, EventType: eventType,
		Status: store.DeliveryAccepted, Payload: body, ReceivedAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			MetricsIncWebhookDuplicate()
			s.dependencies.Logger.Info(
				"github webhook duplicate",
				"delivery_id", deliveryID,
				"event_type", eventType,
			)
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "duplicate", "delivery_id": deliveryID})
			return
		}
		s.writeMappedError(w, r, err)
		return
	}

	// 尽快 202，后台规范化
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

	s.safeGo("webhook_process", func() {
		s.webhookSvc.Process(delivery.ID, eventType, deliveryID, body)
	})
}
