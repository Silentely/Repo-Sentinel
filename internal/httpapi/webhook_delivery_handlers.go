package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// handleListWebhookDeliveries 查询 Webhook 投递历史列表。
func (s *server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	perPage, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("per_page")))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	kind := strings.TrimSpace(r.URL.Query().Get("event_type"))
	repo := strings.TrimSpace(r.URL.Query().Get("repository"))

	items, pageRes, err := s.dependencies.Store.WebhookDeliveries().List(r.Context(), store.ListFilter{
		Page:         page,
		PerPage:      perPage,
		Status:       status,
		Kind:         kind,
		RepositoryID: repo,
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"page":     pageRes.Page,
		"per_page": pageRes.PerPage,
		"total":    pageRes.Total,
	})
}

// handleGetWebhookDelivery 查询指定 Webhook 投递记录与完整 Payload。
func (s *server) handleGetWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	d, err := s.dependencies.Store.WebhookDeliveries().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			d, err = s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), id)
		}
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}

	var payloadObj any
	if len(d.Payload) > 0 {
		_ = json.Unmarshal(d.Payload, &payloadObj)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"delivery":     d,
		"payload_json": payloadObj,
		"payload_raw":  string(d.Payload),
	})
}

// handleReplayWebhookDelivery 一键重放历史 Webhook 投递。
func (s *server) handleReplayWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	d, err := s.dependencies.Store.WebhookDeliveries().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			d, err = s.dependencies.Store.WebhookDeliveries().GetByDeliveryID(r.Context(), id)
		}
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}

	if len(d.Payload) == 0 {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"message": "payload is empty"})
		return
	}

	replayDeliveryID := fmt.Sprintf("%s-replay-%d", d.DeliveryID, time.Now().UnixNano())
	newRecord, err := s.dependencies.Store.WebhookDeliveries().Create(r.Context(), store.WebhookDelivery{
		ID:                 ulid.Make().String(),
		DeliveryID:         replayDeliveryID,
		EventType:          d.EventType,
		Action:             d.Action,
		RepositoryFullName: d.RepositoryFullName,
		Status:             store.DeliveryAccepted,
		Payload:            d.Payload,
		ReceivedAt:         time.Now().UTC(),
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	s.processWebhookAsync(newRecord.ID, newRecord.EventType, newRecord.DeliveryID, newRecord.Payload)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "replayed",
		"id":          newRecord.ID,
		"delivery_id": newRecord.DeliveryID,
	})
}
