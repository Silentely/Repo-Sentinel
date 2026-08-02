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
	// PR 维度过滤下沉到 SQL：客户端对首页 50 条二次过滤会导致总数失真。
	f.ReviewDecision = r.URL.Query().Get("review")
	f.CheckStatus = mapCheckStatusParam(r.URL.Query().Get("check"))
	applyIgnoredFilter(&f, r)
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
	applyIgnoredFilter(&f, r)
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
	applyIgnoredFilter(&f, r)
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

// applyIgnoredFilter 解析 ignored 查询参数：
// - 缺省 / ignored=false：只返回未忽略
// - ignored=true：只返回已忽略
// - ignored=all：返回全部
func applyIgnoredFilter(f *store.ListFilter, r *http.Request) {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("ignored"))) {
	case "true", "1", "yes":
		f.OnlyIgnored = true
	case "all":
		f.IncludeIgnored = true
	}
}

type ignoredBody struct {
	Ignored bool `json:"ignored"`
}

// setResourceIgnored 统一处理本地忽略标记：解析 body → SetIgnored → 回读实体。
func setResourceIgnored[T any](
	s *server,
	w http.ResponseWriter,
	r *http.Request,
	setFn func(ctx context.Context, id string, ignored bool) error,
	getFn func(ctx context.Context, id string) (T, error),
) {
	id := chi.URLParam(r, "id")
	var body ignoredBody
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	if err := setFn(r.Context(), id, body.Ignored); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	item, err := getFn(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) handleSetWorkItemIgnored(w http.ResponseWriter, r *http.Request) {
	setResourceIgnored(s, w, r, s.dependencies.Store.WorkItems().SetIgnored, s.dependencies.Store.WorkItems().Get)
}

func (s *server) handleSetWorkflowRunIgnored(w http.ResponseWriter, r *http.Request) {
	setResourceIgnored(s, w, r, s.dependencies.Store.WorkflowRuns().SetIgnored, s.dependencies.Store.WorkflowRuns().Get)
}

func (s *server) handleSetSecurityAlertIgnored(w http.ResponseWriter, r *http.Request) {
	setResourceIgnored(s, w, r, s.dependencies.Store.SecurityAlerts().SetIgnored, s.dependencies.Store.SecurityAlerts().Get)
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
	channelTypeFilter := r.URL.Query().Get("channel_type")
	// 渠道查询一次即可：既用于把 channel_type 过滤下沉到 SQL（分页/total 才正确），
	// 也用于响应中的渠道类型回填。
	channels, chErr := s.dependencies.Store.Channels().List(r.Context())
	if chErr != nil {
		s.writeMappedError(w, r, chErr)
		return
	}
	if channelTypeFilter != "" {
		ids := make([]string, 0, len(channels))
		for _, ch := range channels {
			if ch.ChannelType == channelTypeFilter {
				ids = append(ids, ch.ID)
			}
		}
		if len(ids) == 0 {
			// 早退分支同样归一化分页参数，保持与其他列表端点响应一致。
			pageNo, perPage := f.Page, f.PerPage
			if pageNo < 1 {
				pageNo = 1
			}
			if perPage < 1 {
				perPage = 20
			}
			if perPage > 100 {
				perPage = 100
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "page": pageNo, "per_page": perPage, "total": 0})
			return
		}
		f.ChannelIDs = ids
	}
	items, page, err := s.dependencies.Store.Outbox().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	chMap := make(map[string]string, len(channels))
	for _, ch := range channels {
		chMap[ch.ID] = ch.ChannelType
	}
	enriched := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"id": item.ID, "channel_id": item.ChannelID, "status": item.Status,
			"title": item.Title, "attempt_count": item.AttemptCount,
			"last_error_code": item.LastErrorCode, "html_url": item.HTMLURL,
			"created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
			"channel_type": chMap[item.ChannelID],
		}
		enriched = append(enriched, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": enriched, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func listFilterFromRequest(r *http.Request) store.ListFilter {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	return store.ListFilter{Page: page, PerPage: perPage}
}

// mapCheckStatusParam 将对外 API 的检查状态词映射为存储值；未知值视为不过滤。
func mapCheckStatusParam(v string) string {
	switch v {
	case "passed":
		return "success"
	case "failed":
		return "failure"
	case "pending":
		return "pending"
	default:
		return ""
	}
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
					// GitHub 侧已归档而本地未归档：联动收口归档状态与能力开关
					//（与 normalizer 的 webhook 侧处理同一语义）。
					if gr.Archived && existing.SyncStatus != store.SyncStatusArchived {
						archived := true
						if uerr := s.dependencies.Store.Repositories().UpdateSettings(r.Context(), existing.ID, store.RepositorySettings{IsArchived: &archived}); uerr == nil {
							in.SyncStatus = store.SyncStatusArchived
						}
					}
					// 本地归档标记不因清单数据抹掉：取消归档仅经 unarchived 事件或设置页操作。
					if existing.IsArchived && !gr.Archived {
						in.IsArchived = true
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
	s.safeGo("reconcile_repo", func() {
		_ = s.dependencies.Reconciler.ReconcileRepository(s.dependencies.Background, repo)
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "repository_id": id})
}

func (s *server) handleReconcileAll(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Reconciler == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "reconcile_unavailable", nil)
		return
	}
	// 全量对账互斥：连点按钮并发多轮会打爆 GitHub 配额并争抢数据库连接。
	if !s.reconcileAllRunning.CompareAndSwap(false, true) {
		s.writeAPIError(w, r, http.StatusConflict, "reconcile_in_progress", nil)
		return
	}
	s.safeGo("reconcile_all", func() {
		defer s.reconcileAllRunning.Store(false)
		_ = s.dependencies.Reconciler.ReconcileAll(s.dependencies.Background, 20)
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}
