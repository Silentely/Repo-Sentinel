package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/go-chi/chi/v5"
)

// tmaRateLimiter 按 (telegram_user_id + IP) 提供内存滑动窗口限流。
type tmaRateLimiter struct {
	mu      sync.Mutex
	history map[string][]time.Time
}

func newTMARateLimiter() *tmaRateLimiter {
	return &tmaRateLimiter{
		history: make(map[string][]time.Time),
	}
}

func (l *tmaRateLimiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-window)
	timestamps := l.history[key]
	valid := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= limit {
		l.history[key] = valid
		return false
	}
	l.history[key] = append(valid, now)
	return true
}

func (s *server) tmaFeatureFlagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.dependencies.Config.HTTP.TMA.Enabled {
			s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) tmaOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedURL, err := url.Parse(strings.TrimSpace(s.dependencies.Config.HTTP.PublicBaseURL))
		if err != nil || expectedURL.Host == "" {
			expectedURL, _ = url.Parse(s.siteOrigin(r))
		}

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			originURL, err := url.Parse(origin)
			if err != nil || originURL.Host == "" || originURL.Scheme == "" {
				s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
				return
			}
			if expectedURL != nil && (!strings.EqualFold(originURL.Scheme, expectedURL.Scheme) || !strings.EqualFold(originURL.Host, expectedURL.Host)) {
				s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
				return
			}
		} else if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
			refURL, err := url.Parse(referer)
			if err == nil && refURL.Host != "" && expectedURL != nil {
				if !strings.EqualFold(refURL.Scheme, expectedURL.Scheme) || !strings.EqualFold(refURL.Host, expectedURL.Host) {
					s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *server) tmaRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		if u, ok := GetTelegramUser(r.Context()); ok && u != nil {
			userID = strconv.FormatInt(u.ID, 10)
		}
		ip := remoteIPFromContext(r.Context())
		if ip == "" {
			ip = r.RemoteAddr
		}

		isRead := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
		limit := 10
		prefix := "w:"
		if isRead {
			limit = 60
			prefix = "r:"
		}

		key := prefix + userID + ":" + ip
		if s.tmaLimiter != nil && !s.tmaLimiter.Allow(key, limit, time.Minute, time.Now()) {
			w.Header().Set("Retry-After", "60")
			s.writeAPIError(w, r, http.StatusTooManyRequests, errorCodeRateLimited, nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *server) getAppClient() *githubx.AppClient {
	if s.dependencies.AppClient != nil {
		return s.dependencies.AppClient
	}
	if s.dependencies.GitHubRuntime != nil {
		return s.dependencies.GitHubRuntime.Client
	}
	return nil
}

func (s *server) resolveInstallationTokenForRepo(ctx context.Context, repo *store.Repository) (string, error) {
	appClient := s.getAppClient()
	if appClient == nil {
		return "", errors.New("github app client unavailable")
	}
	if repo == nil || repo.InstallationID == nil || *repo.InstallationID == "" {
		return "", nil
	}
	raw := *repo.InstallationID
	var installationID int64
	if s.dependencies.Store != nil && s.dependencies.Store.Installations() != nil {
		if inst, err := s.dependencies.Store.Installations().Get(ctx, raw); err == nil && inst.InstallationID > 0 {
			installationID = inst.InstallationID
		} else if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			if inst, err := s.dependencies.Store.Installations().GetByInstallationID(ctx, id); err == nil && inst.InstallationID > 0 {
				installationID = inst.InstallationID
			} else {
				installationID = id
			}
		}
	} else if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		installationID = id
	}
	if installationID <= 0 {
		return "", nil
	}
	return appClient.InstallationToken(ctx, installationID)
}

func (s *server) handleTMADashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.dependencies.Store.Dashboard(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	repos, _, _ := s.dependencies.Store.Repositories().List(r.Context(), store.ListFilter{Page: 1, PerPage: 100})
	monitoredReposCount := 0
	for _, rp := range repos {
		if rp.MonitorEnabled && !rp.IsArchived {
			monitoredReposCount++
		}
	}
	if monitoredReposCount == 0 && stats.ReposActive > 0 {
		monitoredReposCount = stats.ReposActive
	}
	pendingWorkItemsCount := stats.OpenIssues + stats.OpenPulls

	deliveries, page, err := s.dependencies.Store.WebhookDeliveries().List(r.Context(), store.ListFilter{Page: 1, PerPage: 1})
	_ = deliveries
	totalDeliveries := 0
	if err == nil {
		totalDeliveries = page.Total
	}
	successDeliveries, successPage, err := s.dependencies.Store.WebhookDeliveries().List(r.Context(), store.ListFilter{Status: store.DeliveryProcessed, Page: 1, PerPage: 1})
	_ = successDeliveries
	successCount := 0
	if err == nil {
		successCount = successPage.Total
	}
	var deliveryRate float64 = 1.0
	if totalDeliveries > 0 {
		deliveryRate = float64(successCount) / float64(totalDeliveries)
	}

	quietHoursEnabled := false
	quietHoursStart := ""
	quietHoursEnd := ""
	quietHoursTZ := ""
	if ch, chErr := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), store.ChannelTelegram); chErr == nil {
		quietHoursEnabled = ch.QuietHoursEnabled
		quietHoursStart = ch.QuietHoursStart
		quietHoursEnd = ch.QuietHoursEnd
		quietHoursTZ = ch.QuietHoursTZ
	}

	workItems, _, _ := s.dependencies.Store.WorkItems().List(r.Context(), store.ListFilter{Status: "open", Page: 1, PerPage: 20})
	unhandledWorkItems := make([]store.WorkItem, 0)
	for _, it := range workItems {
		if !it.Ignored {
			unhandledWorkItems = append(unhandledWorkItems, it)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"monitored_repos_count":      monitoredReposCount,
		"pending_work_items_count":   pendingWorkItemsCount,
		"webhook_deliveries_total":   totalDeliveries,
		"webhook_deliveries_success": successCount,
		"webhook_delivery_rate":      deliveryRate,
		"quiet_hours_enabled":        quietHoursEnabled,
		"quiet_hours_start":          quietHoursStart,
		"quiet_hours_end":            quietHoursEnd,
		"quiet_hours_tz":             quietHoursTZ,
		"work_items":                 unhandledWorkItems,
	})
}

func (s *server) handleTMAGetEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}

	ev, err := s.dependencies.Store.Events().Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	if ev.RepositoryID == nil || *ev.RepositoryID == "" {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), *ev.RepositoryID)
	if err != nil || !repo.MonitorEnabled || repo.IsArchived {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	var workItem *store.WorkItem
	if ev.SubjectNumber != nil {
		if wi, err := s.dependencies.Store.WorkItems().GetByRepoNumber(r.Context(), repo.ID, int(*ev.SubjectNumber)); err == nil {
			workItem = &wi
		}
	}

	var workflowRun *store.WorkflowRun
	if ev.WorkflowRunID != nil {
		if wr, err := s.dependencies.Store.WorkflowRuns().GetByRepoRunID(r.Context(), repo.ID, *ev.WorkflowRunID); err == nil {
			workflowRun = &wr
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"event":        ev,
		"repository":   repo,
		"work_item":    workItem,
		"workflow_run": workflowRun,
	})
}

func (s *server) handleTMAGetEventDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}

	ev, err := s.dependencies.Store.Events().Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	if ev.RepositoryID == nil || *ev.RepositoryID == "" {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), *ev.RepositoryID)
	if err != nil || !repo.MonitorEnabled || repo.IsArchived {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	if ev.SubjectNumber == nil || *ev.SubjectNumber <= 0 {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_pr"})
		return
	}

	appClient := s.getAppClient()
	if appClient == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	token, err := s.resolveInstallationTokenForRepo(r.Context(), &repo)
	if err != nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	diff, err := appClient.GetPRDiff(r.Context(), token, repo.Owner, repo.Name, int(*ev.SubjectNumber))
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var actorID string
	if user, ok := GetTelegramUser(r.Context()); ok && user != nil {
		actorID = strconv.FormatInt(user.ID, 10)
	}

	s.appendAudit(r.Context(), store.AuditLog{
		Action:     "tma.diff.read",
		ActorType:  "telegram/tma",
		ActorID:    actorID,
		TargetType: "repository",
		TargetID:   repo.ID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"diff": diff,
	})
}

type countingReader struct {
	r         io.Reader
	bytesRead int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.bytesRead += int64(n)
	return n, err
}

func (s *server) handleTMAGetEventLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}

	ev, err := s.dependencies.Store.Events().Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	if ev.RepositoryID == nil || *ev.RepositoryID == "" {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), *ev.RepositoryID)
	if err != nil || !repo.MonitorEnabled || repo.IsArchived {
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	if ev.WorkflowRunID == nil || *ev.WorkflowRunID <= 0 {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_workflow_run"})
		return
	}
	runID := *ev.WorkflowRunID

	appClient := s.getAppClient()
	if appClient == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	token, err := s.resolveInstallationTokenForRepo(r.Context(), &repo)
	if err != nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	jobs, err := appClient.ListWorkflowJobs(r.Context(), token, repo.Owner, repo.Name, runID)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var targetJob *githubx.WorkflowJobItem
	for i := range jobs {
		if jobs[i].Conclusion == "failure" {
			targetJob = &jobs[i]
			break
		}
		for _, step := range jobs[i].Steps {
			if step.Conclusion == "failure" {
				targetJob = &jobs[i]
				break
			}
		}
		if targetJob != nil {
			break
		}
	}
	if targetJob == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"logs":           "",
			"truncated":      false,
			"byte_truncated": false,
			"line_truncated": false,
			"job_id":         int64(0),
			"job_name":       "",
			"status":         "no_failed_job",
		})
		return
	}

	body, err := appClient.DownloadWorkflowJobLogs(r.Context(), token, repo.Owner, repo.Name, targetJob.ID)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	defer body.Close()

	const (
		maxLogBytes   = 2 << 20 // 2 MiB 严格读取上限
		maxLogLines   = 200     // 尾部最多保留 200 行
		maxLineLength = 4096    // 单行最大暂存 4KB，防止单行 3 MiB 挤爆内存
	)
	limitedReader := io.LimitReader(body, maxLogBytes)
	cr := &countingReader{r: limitedReader}
	br := bufio.NewReader(cr)

	ring := make([]string, maxLogLines)
	totalLines := 0

	var curLine strings.Builder
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 {
			if curLine.Len() < maxLineLength {
				remain := maxLineLength - curLine.Len()
				if len(chunk) <= remain {
					curLine.Write(chunk)
				} else {
					curLine.Write(chunk[:remain])
				}
			}
		}
		if !isPrefix {
			if curLine.Len() > 0 || err == nil {
				ring[totalLines%maxLogLines] = curLine.String()
				totalLines++
				curLine.Reset()
			}
		} else if err != nil {
			if curLine.Len() > 0 {
				ring[totalLines%maxLogLines] = curLine.String()
				totalLines++
				curLine.Reset()
			}
		}
		if err != nil {
			break
		}
	}

	byteTruncated := false
	if cr.bytesRead >= maxLogBytes {
		byteTruncated = true
	} else {
		var extra [1]byte
		if n, _ := body.Read(extra[:]); n > 0 {
			byteTruncated = true
		}
	}

	lineTruncated := false
	if totalLines > maxLogLines {
		lineTruncated = true
	}

	truncated := byteTruncated || lineTruncated

	var resultLines []string
	if totalLines <= maxLogLines {
		resultLines = ring[:totalLines]
	} else {
		start := totalLines % maxLogLines
		resultLines = make([]string, maxLogLines)
		for i := 0; i < maxLogLines; i++ {
			resultLines[i] = ring[(start+i)%maxLogLines]
		}
	}
	logs := strings.Join(resultLines, "\n")

	var actorID string
	if user, ok := GetTelegramUser(r.Context()); ok && user != nil {
		actorID = strconv.FormatInt(user.ID, 10)
	}

	s.appendAudit(r.Context(), store.AuditLog{
		Action:     "tma.logs.read",
		ActorType:  "telegram/tma",
		ActorID:    actorID,
		TargetType: "workflow_run",
		TargetID:   strconv.FormatInt(runID, 10),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":           logs,
		"truncated":      truncated,
		"byte_truncated": byteTruncated,
		"line_truncated": lineTruncated,
		"job_id":         targetJob.ID,
		"job_name":       targetJob.Name,
		"status":         "success",
	})
}

type tmaActionRequest struct {
	EventID     string `json:"event_id"`
	Action      string `json:"action"`
	CommentBody string `json:"comment_body,omitempty"`
	// 客户端传入的伪造参数一律忽略，严格以 event_id 反查数据库 (断言 5 & R5)
	Owner       string `json:"owner,omitempty"`
	Repo        string `json:"repo,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	IssueNumber int    `json:"issue_number,omitempty"`
}

func (s *server) handleTMAAction(w http.ResponseWriter, r *http.Request) {
	var req tmaActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	switch req.Action {
	case "workflow_rerun", "pr_quick_approve", "pr_snooze":
	case "pr_comment":
		trimmed := strings.TrimSpace(req.CommentBody)
		if trimmed == "" || len([]rune(trimmed)) > 2000 {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "invalid_comment_body"})
			return
		}
	default:
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "unsupported_action"})
		return
	}

	if strings.TrimSpace(req.EventID) == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "missing_event_id"})
		return
	}

	ev, err := s.dependencies.Store.Events().Get(r.Context(), req.EventID)
	if errors.Is(err, store.ErrNotFound) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
		return
	}
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var actorID string
	if user, ok := GetTelegramUser(r.Context()); ok && user != nil {
		actorID = strconv.FormatInt(user.ID, 10)
	}

	var targetType string
	var targetID string

	recordAudit := func(result, reason, errMsg string) {
		meta := map[string]any{
			"action_result": result,
			"action":        req.Action,
			"event_id":      req.EventID,
		}
		if reason != "" {
			meta["reason"] = reason
		}
		if errMsg != "" {
			meta["error"] = errMsg
		}
		metaJSON, _ := json.Marshal(meta)
		s.appendAudit(r.Context(), store.AuditLog{
			Action:       "tma.action." + req.Action,
			ActorType:    "telegram/tma",
			ActorID:      actorID,
			TargetType:   targetType,
			TargetID:     targetID,
			MetadataJSON: metaJSON,
			IPAddress:    remoteIPFromContext(r.Context()),
		})
	}

	// 严格通过 event_id 反查数据库纳管仓库，忽略客户端传入的任何外部标识 (断言 5 & 6)
	if ev.RepositoryID == nil || *ev.RepositoryID == "" {
		targetType = "repository"
		recordAudit("denied", "missing_repository_id", "")
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	targetType = "repository"
	targetID = *ev.RepositoryID

	repo, err := s.dependencies.Store.Repositories().Get(r.Context(), *ev.RepositoryID)
	if err != nil || !repo.MonitorEnabled || repo.IsArchived {
		recordAudit("denied", "unmonitored_or_archived_repo", "")
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	var stableKey string
	var runID string
	var issueNum int

	switch req.Action {
	case "workflow_rerun":
		if ev.WorkflowRunID == nil || *ev.WorkflowRunID <= 0 {
			recordAudit("rejected", "event_has_no_workflow_run", "")
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_workflow_run"})
			return
		}
		runID = strconv.FormatInt(*ev.WorkflowRunID, 10)
		stableKey = fmt.Sprintf("tma:rerun:%s:%s", repo.ID, runID)
		targetType = "workflow_run"
		targetID = runID
	case "pr_quick_approve":
		if ev.SubjectNumber == nil || *ev.SubjectNumber <= 0 {
			recordAudit("rejected", "event_has_no_pr", "")
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_pr"})
			return
		}
		issueNum = int(*ev.SubjectNumber)
		stableKey = fmt.Sprintf("tma:approve:%s:%d", repo.ID, issueNum)
		targetType = "pull_request"
		targetID = strconv.Itoa(issueNum)
	case "pr_snooze":
		if ev.SubjectNumber == nil || *ev.SubjectNumber <= 0 {
			recordAudit("rejected", "event_has_no_pr", "")
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_pr"})
			return
		}
		issueNum = int(*ev.SubjectNumber)
		stableKey = fmt.Sprintf("tma:snooze:%s:%d", repo.ID, issueNum)
		targetType = "pull_request"
		targetID = strconv.Itoa(issueNum)
	case "pr_comment":
		if ev.SubjectNumber == nil || *ev.SubjectNumber <= 0 {
			recordAudit("rejected", "event_has_no_pr", "")
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": "event_has_no_pr"})
			return
		}
		issueNum = int(*ev.SubjectNumber)
		trimmed := strings.TrimSpace(req.CommentBody)
		sum := sha256.Sum256([]byte(trimmed))
		hashHex := hex.EncodeToString(sum[:])
		if len(hashHex) > 16 {
			hashHex = hashHex[:16]
		}
		stableKey = fmt.Sprintf("tma:comment:%s:%s:%d:%s", actorID, repo.ID, issueNum, hashHex)
		targetType = "pull_request"
		targetID = strconv.Itoa(issueNum)
	}

	actionData := chatops.ActionData{
		ID:          stableKey,
		Action:      req.Action,
		RepoID:      repo.ID,
		RunID:       runID,
		IssueNumber: issueNum,
		Payload:     strings.TrimSpace(req.CommentBody),
		ActorID:     actorID,
	}

	if s.chatOpsService == nil {
		recordAudit("failed", "chatops_service_unavailable", "")
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	outcome, execErr := s.chatOpsService.HandleWeb(r.Context(), actionData)

	if outcome != nil && outcome.Status == chatops.StatusPending {
		recordAudit("conflict", "action_in_progress", "")
		s.writeAPIError(w, r, http.StatusConflict, errorCodeConflict, map[string]any{
			"status": "pending",
			"reason": "action_in_progress",
			"action": req.Action,
		})
		return
	}

	if outcome != nil && outcome.Status == chatops.StatusAlreadyDone {
		recordAudit("already_done", "", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "already_done",
			"message":     outcome.Message,
			"action":      outcome.Action,
			"external_id": outcome.ExternalID,
		})
		return
	}

	if outcome != nil && outcome.Status == chatops.StatusExecuted {
		recordAudit("success", "", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "executed",
			"message":     outcome.Message,
			"action":      outcome.Action,
			"external_id": outcome.ExternalID,
		})
		return
	}

	if outcome != nil && outcome.Status == chatops.StatusForbidden {
		recordAudit("denied", "forbidden", "")
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, nil)
		return
	}

	if outcome != nil && outcome.Status == chatops.StatusGuardrailDenied {
		recordAudit("denied", "guardrail_denied", outcome.Message)
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeForbidden, map[string]any{
			"reason":  "guardrail_denied",
			"message": outcome.Message,
		})
		return
	}

	if outcome != nil && outcome.Status == chatops.StatusIndeterminate {
		recordAudit("failed", "indeterminate", outcome.Message)
		s.writeAPIError(w, r, http.StatusGatewayTimeout, "gateway_timeout", map[string]any{
			"reason":  "indeterminate",
			"message": outcome.Message,
		})
		return
	}

	if execErr != nil {
		recordAudit("failed", "execution_error", execErr.Error())
		s.writeMappedError(w, r, execErr)
		return
	}

	if outcome != nil && outcome.Err != nil {
		recordAudit("failed", "outcome_error", outcome.Err.Error())
		s.writeMappedError(w, r, outcome.Err)
		return
	}

	recordAudit("success", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": outcome.Message,
	})
}

type tmaQuietHoursRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *server) handleTMAToggleQuietHours(w http.ResponseWriter, r *http.Request) {
	var req tmaQuietHoursRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	var actorID string
	if user, ok := GetTelegramUser(r.Context()); ok && user != nil {
		actorID = strconv.FormatInt(user.ID, 10)
	}

	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), store.ChannelTelegram)
	if err != nil {
		s.appendAudit(r.Context(), store.AuditLog{
			Action:       "tma.quiet_hours.toggle",
			ActorType:    "telegram/tma",
			ActorID:      actorID,
			TargetType:   "channel",
			MetadataJSON: []byte(`{"action_result":"failed","reason":"telegram_channel_not_found"}`),
			IPAddress:    remoteIPFromContext(r.Context()),
		})
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, map[string]any{"reason": "telegram_channel_not_found"})
		return
	}

	if err := s.dependencies.Store.Channels().SetQuietHoursEnabled(r.Context(), ch.ID, req.Enabled); err != nil {
		s.appendAudit(r.Context(), store.AuditLog{
			Action:       "tma.quiet_hours.toggle",
			ActorType:    "telegram/tma",
			ActorID:      actorID,
			TargetType:   "channel",
			TargetID:     ch.ID,
			MetadataJSON: []byte(`{"action_result":"failed","reason":"update_failed"}`),
			IPAddress:    remoteIPFromContext(r.Context()),
		})
		s.writeMappedError(w, r, err)
		return
	}

	s.appendAudit(r.Context(), store.AuditLog{
		Action:       "tma.quiet_hours.toggle",
		ActorType:    "telegram/tma",
		ActorID:      actorID,
		TargetType:   "channel",
		TargetID:     ch.ID,
		MetadataJSON: []byte(fmt.Sprintf(`{"action_result":"success","enabled":%t}`, req.Enabled)),
		IPAddress:    remoteIPFromContext(r.Context()),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": req.Enabled,
	})
}
