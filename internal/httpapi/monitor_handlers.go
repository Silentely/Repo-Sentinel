package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
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
	repo, err = s.dependencies.Store.Repositories().Upsert(r.Context(), repo)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *server) handleUpdateRepositorySettings(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body store.RepositorySettings
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	if err := s.dependencies.Store.Repositories().UpdateSettings(r.Context(), id, body); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *server) handleListWorkItems(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = r.URL.Query().Get("kind")
	f.State = r.URL.Query().Get("state")
	f.RepositoryID = r.URL.Query().Get("repository_id")
	// closed 状态应用系统设置的显示限制，避免历史数据无限增长。
	if f.State == "closed" && f.PerPage == 0 {
		if limit := s.getIntSetting(r.Context(), "display.closed_limit", 20); limit > 0 {
			f.PerPage = limit
		}
	}
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
	// dismissed/resolved 状态应用显示限制。
	if f.State != "" && f.State != "open" && f.PerPage == 0 {
		if limit := s.getIntSetting(r.Context(), "display.closed_limit", 20); limit > 0 {
			f.PerPage = limit
		}
	}
	items, page, err := s.dependencies.Store.SecurityAlerts().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

// getIntSetting 从数据库读取整数设置，失败时返回默认值。
func (s *server) getIntSetting(ctx context.Context, key string, defaultVal int) int {
	raw, err := s.dependencies.Store.Settings().Get(ctx, key)
	if err != nil {
		return defaultVal
	}
	var v int
	if json.Unmarshal(raw.ValueJSON, &v) == nil && v > 0 {
		return v
	}
	return defaultVal
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
			"updated_at":        ch.UpdatedAt,
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

func (s *server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if channelType != store.ChannelTelegram && channelType != store.ChannelHTTPWebhook {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	_, err = s.dependencies.Store.Outbox().Create(r.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID,
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title:    "🔔 测试通知",
		BodyText: "🔔 <b>测试通知</b>\n────────────────\n来自 RepoSentinel 的测试消息。\n如果您收到了这条消息，说明通知渠道配置正确！",
		ParseMode: "HTML",
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "channel_type": channelType})
}

func (s *server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if channelType != store.ChannelTelegram && channelType != store.ChannelHTTPWebhook {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if err := s.dependencies.Store.Channels().Delete(r.Context(), ch.ID); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "channel_type": channelType})
}

func (s *server) handleToggleChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if channelType != store.ChannelTelegram && channelType != store.ChannelHTTPWebhook {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		// 如果没有已启用的渠道，尝试获取任意一个
		all, listErr := s.dependencies.Store.Channels().List(r.Context())
		if listErr != nil {
			s.writeMappedError(w, r, listErr)
			return
		}
		found := false
		for _, c := range all {
			if c.ChannelType == channelType {
				ch = c
				found = true
				break
			}
		}
		if !found {
			s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
			return
		}
	}
	if err := s.dependencies.Store.Channels().ToggleEnabled(r.Context(), ch.ID, body.Enabled); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"channel_type":  channelType,
		"enabled":       body.Enabled,
	})
}

func listFilterFromRequest(r *http.Request) store.ListFilter {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	return store.ListFilter{Page: page, PerPage: perPage}
}

func (s *server) handleListInstallations(w http.ResponseWriter, r *http.Request) {
	items, err := s.dependencies.Store.Installations().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleSyncInstallationRepositories 用 Installation Token 拉取 GitHub 上已授权仓库并写入本地（基线状态）。
// 用于补救「installation 事件已到但旧版本未解析 repositories」或主动刷新授权范围。
func (s *server) handleSyncInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	client := (*githubx.AppClient)(nil)
	if s.dependencies.GitHubRuntime != nil && s.dependencies.GitHubRuntime.Client != nil {
		client = s.dependencies.GitHubRuntime.Client
	}
	if client == nil || !client.Configured() {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "github_app_not_configured", nil)
		return
	}
	installations, err := s.dependencies.Store.Installations().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if len(installations) == 0 {
		s.writeAPIError(w, r, http.StatusConflict, "github_no_installation", nil)
		return
	}

	imported := 0
	var lastErr string
	for _, inst := range installations {
		token, err := client.InstallationToken(r.Context(), inst.InstallationID)
		if err != nil {
			lastErr = err.Error()
			s.dependencies.Logger.Warn(
				"installation token failed",
				"installation_id", inst.InstallationID,
				"error", err.Error(),
			)
			continue
		}
		instID := inst.ID
		for page := 1; page <= 20; page++ {
			repos, _, err := client.ListInstallationRepositories(r.Context(), token, page)
			if err != nil {
				lastErr = err.Error()
				s.dependencies.Logger.Warn(
					"list installation repositories failed",
					"installation_id", inst.InstallationID,
					"page", page,
					"error", err.Error(),
				)
				break
			}
			if len(repos) == 0 {
				break
			}
			for _, gr := range repos {
				fullName := strings.TrimSpace(gr.FullName)
				if fullName == "" {
					continue
				}
				owner, name := gr.Owner.Login, gr.Name
				parts := strings.SplitN(fullName, "/", 2)
				if len(parts) == 2 {
					if owner == "" {
						owner = parts[0]
					}
					if name == "" {
						name = parts[1]
					}
				}
				htmlURL := strings.TrimSpace(gr.HTMLURL)
				if htmlURL == "" {
					htmlURL = "https://github.com/" + fullName
				}
				repoID := gr.ID
				in := store.Repository{
					Type:           store.RepositoryTypeInstallation,
					SyncStatus:     store.SyncStatusBaseline,
					GitHubRepoID:   &repoID,
					Owner:          owner,
					Name:           name,
					FullName:       fullName,
					InstallationID: &instID,
					IsArchived:     gr.Archived,
					IsPrivate:      gr.Private,
					HTMLURL:        htmlURL,
					DefaultBranch:  gr.DefaultBranch,
				}
				existing, err := s.dependencies.Store.Repositories().GetByFullName(r.Context(), fullName)
				if err == nil {
					in.ID = existing.ID
					in.SyncStatus = existing.SyncStatus
					if existing.SyncStatus == "" {
						in.SyncStatus = store.SyncStatusBaseline
					}
					if _, err := s.dependencies.Store.Repositories().Upsert(r.Context(), in); err != nil {
						lastErr = err.Error()
						continue
					}
					imported++
					continue
				}
				if err != store.ErrNotFound {
					lastErr = err.Error()
					continue
				}
				now := time.Now().UTC()
				in.BaselineStartedAt = &now
				if _, err := s.dependencies.Store.Repositories().Upsert(r.Context(), in); err != nil {
					lastErr = err.Error()
					continue
				}
				imported++
			}
			if len(repos) < 100 {
				break
			}
		}
	}

	s.dependencies.Logger.Info(
		"github installation repositories synced",
		"request_id", requestIDFromContext(r.Context()),
		"installations", len(installations),
		"imported_or_updated", imported,
	)
	out := map[string]any{
		"installations":       len(installations),
		"imported_or_updated": imported,
	}
	if lastErr != "" && imported == 0 {
		out["last_error"] = lastErr
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	if lastErr != "" {
		out["last_error"] = lastErr
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleReconcileRepository(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Reconciler == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "reconcile_unavailable", nil)
		return
	}
	id := chi.URLParam(r, "id")
	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	go func() {
		_ = s.dependencies.Reconciler.ReconcileRepository(s.dependencies.Background, repo)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "repository_id": id})
}

func (s *server) handleReconcileAll(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Reconciler == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "reconcile_unavailable", nil)
		return
	}
	go func() {
		_ = s.dependencies.Reconciler.ReconcileAll(s.dependencies.Background, 20)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"admin.timezone":              "UTC",
		"digest.local_time":           "09:00",
		"digest.send_empty":           false,
		"notify.aggregate_window_sec": 60,
		"notify.burst_threshold":      15,
		"display.closed_limit":        20,
	}
	for key := range out {
		if s, err := s.dependencies.Store.Settings().Get(r.Context(), key); err == nil {
			var v any
			if json.Unmarshal(s.ValueJSON, &v) == nil {
				out[key] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	allowed := map[string]bool{
		"admin.timezone": true, "digest.local_time": true, "digest.send_empty": true,
		"notify.aggregate_window_sec": true, "notify.burst_threshold": true, "notify.burst_window_sec": true,
		"display.closed_limit": true,
	}
	for k, v := range body {
		if !allowed[k] {
			continue
		}
		raw, _ := json.Marshal(v)
		_, err := s.dependencies.Store.Settings().Upsert(r.Context(), store.SystemSetting{
			ID: ulid.Make().String(), Key: k, ValueJSON: raw, UpdatedAt: time.Now().UTC(), UpdatedBy: "admin",
		})
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	s.handleGetSettings(w, r)
}
