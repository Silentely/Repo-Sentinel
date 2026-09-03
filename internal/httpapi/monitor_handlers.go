package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	stats, err := s.dependencies.Store.Dashboard(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleStarTrend 返回 star 总数按日趋势；days 支持 7/30/90/0（0=全部），非法或缺省回退 30。
func (s *server) handleStarTrend(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && (v == 7 || v == 30 || v == 90 || v == 0) {
			days = v
		}
	}
	items, err := s.dependencies.Store.StarTrend(r.Context(), days)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// writeListResponse 输出统一分页列表响应，五个列表端点共用同一结构。
func writeListResponse[T any](w http.ResponseWriter, items []T, page store.PageResult) {
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func (s *server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = queryTrimmed(r, "type")
	items, page, err := s.dependencies.Store.Repositories().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeListResponse(w, items, page)
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
	if count >= store.MaxExternalRepositories {
		s.writeAPIError(w, r, http.StatusConflict, errorCodeExternalRepoLimit, nil)
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
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "id"})
		return
	}
	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	// 单次 Upsert 写回状态与基线结束时间：Upsert 已会写 SyncStatus，
	// 无需先 UpdateSyncStatus 再 Get 再 Upsert 的三步冗余写。
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

// handleDeleteRepository 彻底删除仓库并级联清理全部关联数据（PR/Issue、事件、告警、快照、
// 游标、待投递通知）。用于 GitHub 侧仓库已删除但 repository.deleted webhook 漏投递
// （或升级前已处理）时的手动收口；删除不可恢复。
func (s *server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "id"})
		return
	}
	if err := s.dependencies.Store.Repositories().DeleteRepository(r.Context(), id); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "repository_id": id})
}

func (s *server) handleUpdateRepositorySettings(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "id"})
		return
	}
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
	f.Kind = queryTrimmed(r, "kind")
	f.State = queryTrimmed(r, "state")
	f.RepositoryID = queryTrimmed(r, "repository_id")
	// PR 维度过滤下沉到 SQL：客户端对首页 50 条二次过滤会导致总数失真。
	f.ReviewDecision = queryTrimmed(r, "review")
	f.CheckStatus = mapCheckStatusParam(r.URL.Query().Get("check"))
	applyIgnoredFilter(&f, r)
	// closed 状态应用系统设置的显示限制，避免历史数据无限增长。
	if f.State == "closed" && f.PerPage == 0 {
		if limit := s.getIntSetting(r.Context(), "display.closed_limit", 20); limit > 0 {
			if limit > 100 {
				limit = 100
			}
			f.PerPage = limit
		}
	}
	items, page, err := s.dependencies.Store.WorkItems().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeListResponse(w, items, page)
}

func (s *server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Status = queryTrimmed(r, "conclusion")
	f.RepositoryID = queryTrimmed(r, "repository_id")
	applyIgnoredFilter(&f, r)
	items, page, err := s.dependencies.Store.WorkflowRuns().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeListResponse(w, items, page)
}

func (s *server) handleListSecurityAlerts(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = queryTrimmed(r, "alert_kind")
	f.State = queryTrimmed(r, "state")
	f.RepositoryID = queryTrimmed(r, "repository_id")
	applyIgnoredFilter(&f, r)
	// dismissed/resolved 状态应用显示限制。
	if f.State != "" && f.State != "open" && f.PerPage == 0 {
		if limit := s.getIntSetting(r.Context(), "display.closed_limit", 20); limit > 0 {
			if limit > 100 {
				limit = 100
			}
			f.PerPage = limit
		}
	}
	items, page, err := s.dependencies.Store.SecurityAlerts().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeListResponse(w, items, page)
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
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "id"})
		return
	}
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

// getIntSetting 读取整数设置，失败时返回默认值（语义与 store.SettingInt 一致）。
func (s *server) getIntSetting(ctx context.Context, key string, defaultVal int) int {
	return store.SettingInt(ctx, s.dependencies.Store.Settings(), key, defaultVal)
}

func (s *server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Kind = queryTrimmed(r, "kind")
	f.RepositoryID = queryTrimmed(r, "repository_id")
	items, page, err := s.dependencies.Store.Events().List(r.Context(), f)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeListResponse(w, items, page)
}

// resolveChannelIDsByType 把 channel_type 过滤参数解析为渠道 ID 集合（列表端点与批量重试共用）。
// 返回空切片表示无任何匹配（调用方按「零结果」处理，不能当「不过滤」）。
func resolveChannelIDsByType(channels []store.NotificationChannel, channelType string) []string {
	ids := make([]string, 0, len(channels))
	for _, ch := range channels {
		if ch.ChannelType == channelType {
			ids = append(ids, ch.ID)
		}
	}
	return ids
}

func (s *server) handleListOutbox(w http.ResponseWriter, r *http.Request) {
	f := listFilterFromRequest(r)
	f.Status = queryTrimmed(r, "status")
	channelTypeFilter := queryTrimmed(r, "channel_type")
	// 渠道查询一次即可：既用于把 channel_type 过滤下沉到 SQL（分页/total 才正确），
	// 也用于响应中的渠道类型回填。
	channels, chErr := s.dependencies.Store.Channels().List(r.Context())
	if chErr != nil {
		s.writeMappedError(w, r, chErr)
		return
	}
	if channelTypeFilter != "" {
		ids := resolveChannelIDsByType(channels, channelTypeFilter)
		if len(ids) == 0 {
			// 早退分支同样归一化分页参数，保持与其他列表端点响应一致。
			normalized := store.NormalizeListFilter(f)
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "page": normalized.Page, "per_page": normalized.PerPage, "total": 0})
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
			// 正文随列表返回：详情抽屉展示通知内容（管理台内部，不做明文过滤）。
			"body_text": item.BodyText,
		}
		enriched = append(enriched, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": enriched, "page": page.Page, "per_page": page.PerPage, "total": page.Total})
}

func queryTrimmed(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func listFilterFromRequest(r *http.Request) store.ListFilter {
	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	perPage, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("per_page")))
	return store.ListFilter{Page: page, PerPage: perPage}
}

// mapCheckStatusParam 将对外 API 的检查状态词映射为存储值；未知值视为不过滤。
func mapCheckStatusParam(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "passed", "success":
		return "success"
	case "failed", "failure":
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

// handleSyncInstallationRepositories 触发安装仓库清单同步（基线状态）。
// 同步逻辑在 syncx.Reconciler.SyncInstallations，handler 仅做配置校验与结果映射。
func (s *server) handleSyncInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	// Reconciler 持有 GitHub App client；nil 视为未装配/未配置。
	if s.dependencies.Reconciler == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeGitHubAppNotConfigured, nil)
		return
	}
	result, err := s.dependencies.Reconciler.SyncInstallations(r.Context(), 20)
	if err != nil {
		switch {
		case errors.Is(err, syncx.ErrAppNotConfigured):
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeGitHubAppNotConfigured, nil)
		case errors.Is(err, syncx.ErrNoInstallation):
			s.writeAPIError(w, r, http.StatusConflict, errorCodeGitHubNoInstallation, nil)
		default:
			s.writeMappedError(w, r, err)
		}
		return
	}
	s.dependencies.Logger.Info(
		"github installation repositories synced",
		"request_id", requestIDFromContext(r.Context()),
		"installations", result.Installations,
		"imported_or_updated", result.Imported,
	)
	out := map[string]any{
		"installations":       result.Installations,
		"imported_or_updated": result.Imported,
	}
	if result.LastError != "" {
		out["last_error"] = result.LastError
		if result.Imported == 0 {
			writeJSON(w, http.StatusBadGateway, out)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleReconcileRepository(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.Reconciler == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeReconcileUnavailable, nil)
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "id"})
		return
	}
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
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeReconcileUnavailable, nil)
		return
	}
	// 全量对账互斥：连点按钮并发多轮会打爆 GitHub 配额并争抢数据库连接。
	if !s.reconcileAllRunning.CompareAndSwap(false, true) {
		s.writeAPIError(w, r, http.StatusConflict, errorCodeReconcileInProgress, nil)
		return
	}
	s.safeGo("reconcile_all", func() {
		defer s.reconcileAllRunning.Store(false)
		_ = s.dependencies.Reconciler.ReconcileAll(s.dependencies.Background, 20)
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}
