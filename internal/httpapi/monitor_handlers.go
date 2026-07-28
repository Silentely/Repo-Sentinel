package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	stats, err := s.dependencies.Store.Dashboard(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = r.URL.Query().Get("type")
	items, page, err := s.dependencies.Store.Repositories().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleAddExternalRepository(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FullName string `json:"full_name"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	fullName := strings.Trim(strings.TrimSpace(body.FullName), "/")
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "full_name"})
		return
	}
	count, err := s.dependencies.Store.Repositories().CountByType(r.Context(), store.RepositoryTypeExternal)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if count >= 20 {
		s.writeAPIError(w, r, http.StatusConflict, "external_repo_limit", nil)
		return
	}
	now := time.Now().UTC()
	repo, err := s.dependencies.Store.Repositories().Upsert(r.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusBaseline,
		Owner: parts[0], Name: parts[1], FullName: fullName,
		HTMLURL: "https://github.com/" + fullName, BaselineStartedAt: &now,
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *server) handleActivateRepository(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.dependencies.Store.Repositories().UpdateSyncStatus(r.Context(), id, store.SyncStatusActive); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	now := time.Now().UTC()
	repo.BaselineFinishedAt = &now
	repo.SyncStatus = store.SyncStatusActive
	repo, _ = s.dependencies.Store.Repositories().Upsert(r.Context(), repo)
	writeJSON(w, http.StatusOK, repo)
}

func (s *server) handleListWorkItems(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = r.URL.Query().Get("kind")
	f.State = r.URL.Query().Get("state")
	f.RepositoryID = r.URL.Query().Get("repository_id")
	items, page, err := s.dependencies.Store.WorkItems().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Status = r.URL.Query().Get("conclusion")
	f.RepositoryID = r.URL.Query().Get("repository_id")
	items, page, err := s.dependencies.Store.WorkflowRuns().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListSecurityAlerts(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = r.URL.Query().Get("alert_kind")
	f.State = r.URL.Query().Get("state")
	f.RepositoryID = r.URL.Query().Get("repository_id")
	items, page, err := s.dependencies.Store.SecurityAlerts().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = r.URL.Query().Get("kind")
	f.RepositoryID = r.URL.Query().Get("repository_id")
	items, page, err := s.dependencies.Store.Events().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListOutbox(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Status = r.URL.Query().Get("status")
	items, page, err := s.dependencies.Store.Outbox().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	// 不返回 body 全量时可裁剪；MVP 直接返回
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	masked := make([]map[string]any, 0, len(items))
	for _, ch := range items {
		masked = append(masked, map[string]any{
			"id": ch.ID, "channel_type": ch.ChannelType, "name": ch.Name,
			"enabled": ch.Enabled, "target": ch.Target, "allow_private": ch.AllowPrivate,
			"secret_configured": ch.SecretEnvelope != "",
			"updated_at": ch.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": masked})
}

func (s *server) handleUpsertChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if channelType != store.ChannelTelegram && channelType != store.ChannelHTTPWebhook {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	var body struct {
		Name         string `json:"name"`
		Enabled      bool   `json:"enabled"`
		Target       string `json:"target"`
		Secret       string `json:"secret"`
		AllowPrivate bool   `json:"allow_private"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	existing, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	ch := store.NotificationChannel{
		ChannelType: channelType, Name: body.Name, Enabled: body.Enabled,
		Target: body.Target, AllowPrivate: body.AllowPrivate,
	}
	if err == nil {
		ch.ID = existing.ID
		ch.SecretEnvelope = existing.SecretEnvelope
	}
	if body.Secret != "" {
		if s.dependencies.KeyRing == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, "encryption_unavailable", nil)
			return
		}
		env, err := s.dependencies.KeyRing.Encrypt(r.Context(), []byte(body.Secret), []byte("reposentinel:notify-secret:v1"))
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		ch.SecretEnvelope = env
	}
	// 环境变量引导：若未提供 secret 且 telegram 配置有 token
	if channelType == store.ChannelTelegram && ch.SecretEnvelope == "" {
		if tok := s.dependencies.Config.Notify.Telegram.Token.Reveal(); tok != "" {
			if s.dependencies.KeyRing != nil {
				if env, err := s.dependencies.KeyRing.Encrypt(r.Context(), []byte(tok), []byte("reposentinel:notify-secret:v1")); err == nil {
					ch.SecretEnvelope = env
				}
			}
		}
		if ch.Target == "" {
			ch.Target = s.dependencies.Config.Notify.Telegram.ChatID
		}
	}
	saved, err := s.dependencies.Store.Channels().Upsert(r.Context(), ch)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if saved.Enabled {
		_ = s.dependencies.Store.Channels().DisableOthersOfType(r.Context(), channelType, saved.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": saved.ID, "channel_type": saved.ChannelType, "enabled": saved.Enabled,
		"target": saved.Target, "secret_configured": saved.SecretEnvelope != "",
	})
}

func (s *server) handleRetryOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.dependencies.Store.Outbox().RetryDead(r.Context(), id, time.Now().UTC()); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "id": id})
}

func listFilterFromRequest(r *http.Request) store.ListFilter {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	return store.ListFilter{Page: page, PerPage: perPage}
}
