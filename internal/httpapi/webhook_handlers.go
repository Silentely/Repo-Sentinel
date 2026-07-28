package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
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

	secrets := make([]string, 0, 2)
	if v := s.dependencies.Config.GitHub.WebhookSecret.Reveal(); v != "" {
		secrets = append(secrets, v)
	}
	if v := s.dependencies.Config.GitHub.WebhookPreviousSecret.Reveal(); v != "" {
		secrets = append(secrets, v)
	}
	if len(secrets) == 0 {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "webhook_not_configured", nil)
		return
	}
	if !githubx.VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), secrets...) {
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
		if err == store.ErrConflict {
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "duplicate", "delivery_id": deliveryID})
			return
		}
		s.writeMappedError(w, r, err)
		return
	}

	// 尽快 202，后台规范化
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":      "accepted",
		"delivery_id": deliveryID,
	})

	go s.processWebhookAsync(delivery.ID, eventType, deliveryID, body)
}

func (s *server) processWebhookAsync(rowID, eventType, deliveryID string, body []byte) {
	ctx := s.dependencies.Background
	if ctx == nil {
		return
	}
	proc := &normalizer.Processor{Store: s.dependencies.Store}
	res, err := proc.Process(ctx, eventType, deliveryID, body)
	if err != nil {
		_ = s.dependencies.Store.WebhookDeliveries().MarkProcessed(ctx, rowID, store.DeliveryFailed, "normalize_failed")
		s.dependencies.Logger.Error("webhook normalize failed", "delivery_id", deliveryID, "error_code", "normalize_failed")
		return
	}
	repoName := ""
	if res.Repository != nil {
		repoName = res.Repository.FullName
	}
	if res.Event != nil && !res.SuppressNotify {
		engine := &rules.Engine{Store: s.dependencies.Store}
		if err := engine.Evaluate(ctx, res, repoName); err != nil {
			s.dependencies.Logger.Error("rule evaluate failed", "delivery_id", deliveryID, "error_code", "rule_failed")
		}
	}
	_ = s.dependencies.Store.WebhookDeliveries().MarkProcessed(ctx, rowID, store.DeliveryProcessed, "")
}
