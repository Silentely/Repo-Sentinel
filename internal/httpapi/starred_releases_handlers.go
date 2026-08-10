package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

// starredReleasesConfigResponse star release 追踪配置视图。
type starredReleasesConfigResponse struct {
	Username                string         `json:"username"`
	StarSyncInterval        string         `json:"star_sync_interval"`
	ReleasePollInterval     string         `json:"release_poll_interval"`
	MaxTrackers             int            `json:"max_trackers"`
	NotifyPrerelease        bool           `json:"notify_prerelease"`
	Enabled                 bool           `json:"enabled"`
	AIReleaseSummaryEnabled bool           `json:"ai_release_summary_enabled"`
	Counts                  map[string]int `json:"counts"`
}

type starredReleasesConfigPutRequest struct {
	Username            *string `json:"username"`
	StarSyncInterval    *string `json:"star_sync_interval"`
	ReleasePollInterval *string `json:"release_poll_interval"`
	MaxTrackers         *int    `json:"max_trackers"`
	NotifyPrerelease    *bool   `json:"notify_prerelease"`
	Enabled             *bool   `json:"enabled"`
}

// normalizeUsername 归一化 GitHub 用户名：容忍粘贴 URL 与 @ 前缀。
func normalizeUsername(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "@")
	for _, prefix := range []string{"https://github.com/", "http://github.com/"} {
		v = strings.TrimPrefix(v, prefix)
	}
	return strings.TrimSuffix(v, "/")
}

// handleGetStarredReleasesConfig 返回配置与追踪状态概览。
func (s *server) handleGetStarredReleasesConfig(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	ctx := r.Context()
	settings := s.dependencies.Store.Settings()
	counts, err := s.dependencies.Store.StarredTrackers().CountByState(ctx)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	resp := starredReleasesConfigResponse{
		Username:            syncx.StarredUsername(ctx, settings),
		StarSyncInterval:    syncx.StarSyncInterval(ctx, settings).String(),
		ReleasePollInterval: syncx.ReleasePollInterval(ctx, settings).String(),
		MaxTrackers:         syncx.MaxTrackers(ctx, settings),
		NotifyPrerelease:    syncx.NotifyPrerelease(ctx, settings),
		Enabled:             store.FeatureEnabled(ctx, settings, store.SettingFeatureStarredReleases),
		Counts:              counts,
	}
	if rt := s.aiRuntime(); rt != nil {
		resp.AIReleaseSummaryEnabled = rt.Snapshot().ReleaseSummaryEnabled
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePutStarredReleasesConfig 保存配置；用户名变更后立即触发一轮 star 同步。
func (s *server) handlePutStarredReleasesConfig(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	var body starredReleasesConfigPutRequest
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	settings := s.dependencies.Store.Settings()
	now := time.Now().UTC()
	upsert := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = settings.Upsert(ctx, store.SystemSetting{
			ID: ulid.Make().String(), Key: key, ValueJSON: raw, UpdatedAt: now, UpdatedBy: "admin",
		})
		return err
	}
	usernameChanged := false
	if body.Username != nil {
		if err := upsert(syncx.SettingStarredUsername, normalizeUsername(*body.Username)); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		usernameChanged = true
	}
	if body.StarSyncInterval != nil {
		d, err := time.ParseDuration(*body.StarSyncInterval)
		if err != nil || d < time.Minute || d > 30*24*time.Hour {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "star_sync_interval"})
			return
		}
		if err := upsert(syncx.SettingStarSyncInterval, d.String()); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	if body.ReleasePollInterval != nil {
		d, err := time.ParseDuration(*body.ReleasePollInterval)
		if err != nil || d < time.Minute || d > 24*time.Hour {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "release_poll_interval"})
			return
		}
		if err := upsert(syncx.SettingReleasePollInterval, d.String()); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	if body.MaxTrackers != nil {
		if *body.MaxTrackers < 1 || *body.MaxTrackers > 10000 {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "max_trackers"})
			return
		}
		if err := upsert(syncx.SettingMaxTrackers, *body.MaxTrackers); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	if body.NotifyPrerelease != nil {
		if err := upsert(syncx.SettingNotifyPrerelease, *body.NotifyPrerelease); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	if body.Enabled != nil {
		if err := upsert(store.SettingFeatureStarredReleases, *body.Enabled); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	// 用户名变更即时生效，不必等 star 同步周期。
	if usernameChanged && s.dependencies.StarredPoller != nil {
		if err := s.dependencies.StarredPoller.SyncStars(ctx); err != nil {
			s.dependencies.Logger.Warn("starred releases sync after config failed", "error_code", "star_sync_failed", "error", err.Error())
		}
	}
	s.handleGetStarredReleasesConfig(w, r)
}

// handleSyncStarredReleases 立即执行一轮 star 同步。
func (s *server) handleSyncStarredReleases(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil || s.dependencies.StarredPoller == nil {
		writeJSON(w, http.StatusOK, map[string]any{"started": false})
		return
	}
	if syncx.StarredUsername(r.Context(), s.dependencies.Store.Settings()) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"started": false})
		return
	}
	if err := s.dependencies.StarredPoller.SyncStars(r.Context()); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

// starredTrackerItem 管理台追踪列表条目。
type starredTrackerItem struct {
	ID                     string     `json:"id"`
	FullName               string     `json:"full_name"`
	State                  string     `json:"state"`
	LastReleaseTag         string     `json:"last_release_tag,omitempty"`
	LastReleasePublishedAt *time.Time `json:"last_release_published_at,omitempty"`
	LastPollAt             *time.Time `json:"last_poll_at,omitempty"`
	FirstSeenAt            time.Time  `json:"first_seen_at"`
}

// handleListStarredTrackers 追踪列表（分页 + state 筛选）。
func (s *server) handleListStarredTrackers(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	f := listFilterFromRequest(r)
	f.State = r.URL.Query().Get("state")
	items, page, err := s.dependencies.Store.StarredTrackers().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	out := make([]starredTrackerItem, 0, len(items))
	for _, it := range items {
		out = append(out, starredTrackerItem{
			ID: it.ID, FullName: it.FullName, State: it.State,
			LastReleaseTag:         it.LastReleaseTag,
			LastReleasePublishedAt: it.LastReleasePublishedAt,
			LastPollAt:             it.LastPollAt,
			FirstSeenAt:            it.FirstSeenAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

// handleSetStarredTrackerState 单仓停用 / 恢复（仅允许 disabled 与 tracking 两个目标态）。
func (s *server) handleSetStarredTrackerState(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	if body.State != store.TrackerStateDisabled && body.State != store.TrackerStateTracking {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "state"})
		return
	}
	if err := s.dependencies.Store.StarredTrackers().UpdateState(r.Context(), chi.URLParam(r, "id"), body.State); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
