package syncx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Reconciler 自有仓库 API 对账与首次基线。
type Reconciler struct {
	Store  store.Store
	GitHub *githubx.AppClient
	Logger *slog.Logger
	// 单次仓库请求预算（防打爆 API）
	MaxPages int
	// OnRun 可选：每次对账执行回调（指标等）。
	OnRun func()
}

// ReconcileRepository 对单仓执行增量或基线同步。
func (r *Reconciler) ReconcileRepository(ctx context.Context, repo store.Repository) error {
	if r.MaxPages <= 0 {
		r.MaxPages = 3
	}
	if repo.Type != store.RepositoryTypeInstallation {
		return nil
	}
	// 监控总开关关闭或仓库已归档：不再采集，与设置页开关语义一致。
	if !repo.MonitorEnabled || repo.IsArchived {
		return nil
	}
	if repo.InstallationID == nil || *repo.InstallationID == "" {
		return fmt.Errorf("missing_installation")
	}
	if r.GitHub == nil || !r.GitHub.Configured() {
		return fmt.Errorf("github_app_not_configured")
	}
	inst, err := r.Store.Installations().Get(ctx, *repo.InstallationID)
	if err != nil {
		if id, e := strconv.ParseInt(*repo.InstallationID, 10, 64); e == nil {
			inst, err = r.Store.Installations().GetByInstallationID(ctx, id)
		}
	}
	if err != nil {
		// 回退：若仅有一个 installation，用于单 App 部署
		all, listErr := r.Store.Installations().List(ctx)
		if listErr != nil || len(all) != 1 {
			return fmt.Errorf("installation_lookup: %w", err)
		}
		inst = all[0]
	}
	token, err := r.GitHub.InstallationToken(ctx, inst.InstallationID)
	if err != nil {
		return err
	}

	isBaseline := repo.SyncStatus == store.SyncStatusBaseline
	var since *time.Time
	if !isBaseline {
		if cur, err := r.Store.Cursors().Get(ctx, repo.ID, "issues"); err == nil && cur.CursorValue != "" {
			if t, e := time.Parse(time.RFC3339, cur.CursorValue); e == nil {
				// 重叠 30 秒
				t = t.Add(-30 * time.Second)
				since = &t
			}
		}
	}

	// 全局功能开关：与「关于与设置 → 功能模块」一致，关闭后整类停止对账。
	features := store.LoadFeatureFlags(ctx, r.Store.Settings())
	wantIssues := features.Issues && repo.IssuesEnabled
	wantPRs := features.PullRequests && repo.PrEnabled
	wantActions := features.Actions && repo.ActionsEnabled
	wantAlerts := features.SecurityAlerts && repo.AlertsEnabled

	// Issues 与 PR 共用 GitHub Issues API：任一仍开启才需要拉取。
	if wantIssues || wantPRs {
		if err := r.syncIssues(ctx, token, repo, since, isBaseline, wantIssues, wantPRs); err != nil {
			return err
		}
	}
	softFailed := false
	if wantActions {
		if err := r.syncWorkflows(ctx, token, repo, isBaseline); err != nil {
			// Actions 权限不足时软失败
			softFailed = true
			if r.Logger != nil {
				r.Logger.Warn("workflow reconcile soft-fail", "repo", repo.FullName, "error_code", "reconcile_workflows", "error", err.Error())
			}
		}
	}
	if wantAlerts {
		for _, kind := range []string{store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning} {
			if err := r.syncAlerts(ctx, token, repo, kind, isBaseline); err != nil {
				if alertUnavailable(kind, err) {
					// 仓库未开启该功能（或 App 未授权）：静态配置反复对账结果不变，静默跳过。
					if r.Logger != nil {
						r.Logger.Debug("alerts feature unavailable, skip", "repo", repo.FullName, "kind", kind, "error", err.Error())
					}
					continue
				}
				softFailed = true
				if r.Logger != nil {
					r.Logger.Warn("alerts reconcile soft-fail", "repo", repo.FullName, "kind", kind, "error_code", "reconcile_alerts", "error", err.Error())
				}
			}
		}
	}

	now := time.Now().UTC()
	repo.LastSyncedAt = &now
	// 部分失败不能静默抹平：记录痕迹供管理台提示权限不足。
	repo.LastSyncErrorCode = ""
	if softFailed {
		repo.LastSyncErrorCode = "reconcile_partial"
	}
	if isBaseline {
		repo.SyncStatus = store.SyncStatusActive
		repo.BaselineFinishedAt = &now
	}
	_, _ = r.Store.Repositories().Upsert(ctx, repo)
	_, _ = r.Store.Cursors().Upsert(ctx, store.SyncCursor{
		RepositoryID: repo.ID, Resource: "issues", CursorValue: now.Format(time.RFC3339),
		LastSuccessAt: &now,
	})
	return nil
}

// 单仓单轮对账的 PR enrich 预算（每个 PR 需要 reviews/reviewers/detail/check-runs 共 4 次 API 调用）。
// PR 密集的仓库若无预算约束，一轮对账即可耗尽 installation token 的 5000 次/时配额。
const prEnrichBudgetPerRound = 25

func (r *Reconciler) syncIssues(ctx context.Context, token string, repo store.Repository, since *time.Time, baseline, wantIssues, wantPRs bool) error {
	enrichBudget := prEnrichBudgetPerRound
	for page := 1; page <= r.MaxPages; page++ {
		items, remaining, err := r.GitHub.ListIssues(ctx, token, repo.Owner, repo.Name, since, page)
		if err != nil {
			return err
		}
		if remaining >= 0 && remaining < 50 {
			if r.Logger != nil {
				r.Logger.Info("github rate low", "remaining", remaining, "repo", repo.FullName)
			}
		}
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			kind := store.WorkItemKindIssue
			if it.PullRequest != nil {
				kind = store.WorkItemKindPR
			}
			// Issues API 混合返回 issue 与 PR：按全局 + 仓库级开关决定采集谁。
			if kind == store.WorkItemKindIssue && !wantIssues {
				continue
			}
			if kind == store.WorkItemKindPR && !wantPRs {
				continue
			}
			labels := make([]any, 0, len(it.Labels))
			for _, l := range it.Labels {
				labels = append(labels, l.Name)
			}
			assignees := make([]any, 0, len(it.Assignees))
			for _, a := range it.Assignees {
				assignees = append(assignees, a.Login)
			}
			milestone := ""
			if it.Milestone != nil {
				milestone = it.Milestone.Title
			}
			hash := normalizer.StateHash(kind, it.State, it.Title, it.User.Login, milestone, strconv.FormatBool(it.Draft))
			item := store.WorkItem{
				RepositoryID: repo.ID, Number: it.Number, Kind: kind, State: it.State, Title: it.Title,
				Author: it.User.Login, LabelsJSON: labels, AssigneesJSON: assignees, Milestone: milestone,
				Draft: it.Draft, HTMLURL: it.HTMLURL, SourceUpdatedAt: it.UpdatedAt, StateHash: hash,
			}

			// PR 审核/检查状态 enrich（仅对 PR 且非基线同步）：
			// 每次调用需 4 次 GitHub API，对新/变化/检查进行中的 PR 才触发，且受单轮预算约束。
			if kind == store.WorkItemKindPR && !baseline {
				existing, _ := r.Store.WorkItems().GetByRepoNumber(ctx, repo.ID, it.Number)
				if existing.ID != "" {
					// 先沿用已存储的审核/检查字段：UpsertIfNewer 用入参全量覆盖，
					// 预算耗尽跳过 enrich 时不能把已有数据清零。
					item.ReviewState = existing.ReviewState
					item.ReviewDecision = existing.ReviewDecision
					item.Reviewers = existing.Reviewers
					item.CheckStatus = existing.CheckStatus
					item.CheckConclusion = existing.CheckConclusion
					item.ChecksTotal = existing.ChecksTotal
					item.ChecksPassed = existing.ChecksPassed
				}
				stale := existing.ID == "" || existing.StateHash != hash || existing.CheckStatus == "pending"
				if stale && enrichBudget > 0 {
					enrichBudget--
					r.enrichPullRequest(ctx, token, repo, it.Number, &item)
				}
			}

			saved, updated, err := r.Store.WorkItems().UpsertIfNewer(ctx, item)
			if err != nil || !updated || baseline {
				continue
			}
			fp := normalizer.Fingerprint("reconcile", repo.FullName, kind, normalizer.ResourceIdentity(kind, saved.Number, 0), "reconcile", saved.SourceUpdatedAt, hash)
			if _, err := r.Store.Events().GetByFingerprint(ctx, fp); err == nil {
				continue
			}
			num := saved.Number
			src := saved.SourceUpdatedAt
			_, _ = r.Store.Events().Create(ctx, store.Event{
				ID: ulid.Make().String(), Source: "reconcile", Kind: kind, Action: "updated",
				RepositoryID: &repo.ID, SubjectNumber: &num, Title: saved.Title, Actor: saved.Author,
				OccurredAt: saved.SourceUpdatedAt, SourceUpdatedAt: &src, HTMLURL: saved.HTMLURL,
				SuppressNotification: false, DedupeFingerprint: fp, StateHash: hash,
				PayloadSummary: map[string]any{"state": saved.State},
			})
		}
		if len(items) < 100 {
			break
		}
	}
	return nil
}

// enrichPullRequest 拉取 PR 的审核结论、评审人与检查状态并回填到 item。
// 尽力而为：单个 API 失败只记日志，不清空已从现有记录沿用的字段。
func (r *Reconciler) enrichPullRequest(ctx context.Context, token string, repo store.Repository, number int, item *store.WorkItem) {
	// 获取 Review 状态
	reviews, reviewErr := r.GitHub.ListPRReviews(ctx, token, repo.Owner, repo.Name, number)
	if reviewErr == nil && len(reviews) > 0 {
		// 找到最新的 Review（按 SubmittedAt 时间排序）
		latestReview := reviews[0]
		for _, rv := range reviews[1:] {
			if rv.SubmittedAt.After(latestReview.SubmittedAt) {
				latestReview = rv
			}
		}
		item.ReviewState = latestReview.State
		// 计算审核决策
		if latestReview.State == "APPROVED" {
			item.ReviewDecision = "approved"
		} else if latestReview.State == "CHANGES_REQUESTED" {
			item.ReviewDecision = "changes_requested"
		}
	}

	// 获取 Requested Reviewers
	requestedReviewers, reviewersErr := r.GitHub.ListRequestedReviewers(ctx, token, repo.Owner, repo.Name, number)
	if reviewersErr == nil {
		item.Reviewers = requestedReviewers
	}

	// 获取 PR 详情以获取 head SHA（Issues API 不返回此字段）
	prDetail, prErr := r.GitHub.GetPRDetail(ctx, token, repo.Owner, repo.Name, number)
	if prErr == nil && prDetail.Head.SHA != "" {
		// 获取 Check Runs
		checkRuns, checkErr := r.GitHub.ListCheckRuns(ctx, token, repo.Owner, repo.Name, prDetail.Head.SHA)
		if checkErr == nil {
			item.ChecksTotal = len(checkRuns)
			passed := 0
			hasFailure := false
			for _, cr := range checkRuns {
				if cr.Conclusion == "success" {
					passed++
				} else if cr.Conclusion == "failure" || cr.Conclusion == "timed_out" {
					hasFailure = true
				}
			}
			item.ChecksPassed = passed
			if hasFailure {
				item.CheckStatus = "failure"
				item.CheckConclusion = "failure"
			} else if passed == len(checkRuns) {
				item.CheckStatus = "success"
				item.CheckConclusion = "success"
			} else {
				item.CheckStatus = "pending"
				item.CheckConclusion = "pending"
			}
		}
	}
}

func (r *Reconciler) syncWorkflows(ctx context.Context, token string, repo store.Repository, baseline bool) error {
	for page := 1; page <= r.MaxPages; page++ {
		items, _, err := r.GitHub.ListWorkflowRuns(ctx, token, repo.Owner, repo.Name, page)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		for _, run := range items {
			hash := normalizer.StateHash(strconv.FormatInt(run.ID, 10), run.Status, strPtr(run.Conclusion), strconv.Itoa(run.RunAttempt), run.HeadSHA)
			in := store.WorkflowRun{
				RepositoryID: repo.ID, GitHubRunID: run.ID, GitHubWorkflowID: run.WorkflowID,
				WorkflowName: run.Name, RunNumber: run.RunNumber, Event: run.Event, HeadBranch: run.HeadBranch,
				HeadSHA: run.HeadSHA, Status: run.Status, Conclusion: run.Conclusion, Actor: run.Actor.Login,
				RunAttempt: run.RunAttempt, HTMLURL: run.HTMLURL, RunUpdatedAt: run.UpdatedAt, StateHash: hash,
			}
			_, _, _ = r.Store.WorkflowRuns().UpsertIfNewer(ctx, in)
			_ = baseline
		}
		if len(items) < 50 {
			break
		}
	}
	return nil
}

func (r *Reconciler) syncAlerts(ctx context.Context, token string, repo store.Repository, kind string, baseline bool) error {
	switch kind {
	case store.AlertKindDependabot:
		return r.syncDependabotAlerts(ctx, token, repo, baseline)
	case store.AlertKindCodeScanning:
		return r.syncPageAlerts(ctx, token, repo, kind, baseline)
	case store.AlertKindSecretScanning:
		return r.syncPageAlerts(ctx, token, repo, kind, baseline)
	}
	return nil
}

// syncDependabotAlerts 用游标分页拉取 dependabot alerts。
func (r *Reconciler) syncDependabotAlerts(ctx context.Context, token string, repo store.Repository, baseline bool) error {
	var cursor string
	for page := 1; page <= r.MaxPages; page++ {
		items, next, _, err := r.GitHub.ListDependabotAlerts(ctx, token, repo.Owner, repo.Name, cursor)
		if err != nil {
			return err
		}
		r.saveAlerts(ctx, repo, store.AlertKindDependabot, items)
		if next == "" {
			break
		}
		cursor = next
		_ = baseline
	}
	return nil
}

// syncPageAlerts 用传统 page 分页拉取 code_scanning / secret_scanning alerts。
func (r *Reconciler) syncPageAlerts(ctx context.Context, token string, repo store.Repository, kind string, baseline bool) error {
	var listFn func(context.Context, string, string, string, int) ([]githubx.AlertItem, int, error)
	switch kind {
	case store.AlertKindCodeScanning:
		listFn = r.GitHub.ListCodeScanningAlerts
	case store.AlertKindSecretScanning:
		listFn = r.GitHub.ListSecretScanningAlerts
	default:
		return nil
	}
	for page := 1; page <= r.MaxPages; page++ {
		items, _, err := listFn(ctx, token, repo.Owner, repo.Name, page)
		if err != nil {
			return err
		}
		r.saveAlerts(ctx, repo, kind, items)
		if len(items) < 50 {
			break
		}
		_ = baseline
	}
	return nil
}

// saveAlerts 将告警列表写入 store。
func (r *Reconciler) saveAlerts(ctx context.Context, repo store.Repository, kind string, all []githubx.AlertItem) {
	for _, a := range all {
		severity := a.Severity
		rule := ""
		switch kind {
		case store.AlertKindDependabot:
			if a.Dependency != nil {
				rule = a.Dependency.Package.Name
			}
			if a.SecurityAdvisory != nil && severity == "" {
				severity = a.SecurityAdvisory.Severity
			}
		case store.AlertKindCodeScanning:
			if a.Rule != nil {
				rule = a.Rule.ID
				if severity == "" {
					severity = a.Rule.Severity
				}
			}
		case store.AlertKindSecretScanning:
			rule = a.SecretTypeDisplayName
			if rule == "" {
				rule = a.SecretType
			}
		}
		updated := a.UpdatedAt
		if updated.IsZero() {
			updated = a.CreatedAt
		}
		hash := normalizer.StateHash(kind, a.State, severity, rule, a.DismissedReason)
		_, _, _ = r.Store.SecurityAlerts().UpsertIfNewer(ctx, store.SecurityAlert{
			RepositoryID: repo.ID, AlertKind: kind, AlertNumber: a.Number, State: a.State,
			Severity: severity, RuleOrDependency: rule, DismissedReason: a.DismissedReason,
			HTMLURL: a.HTMLURL, SourceUpdatedAt: updated, StateHash: hash,
		})
	}
}

// alertUnavailable 判断告警 API 的错误是否为"功能未开启"不可恢复状态。
// 功能未开启的仓库反复对账永远是同一结果，应静默跳过而非标记 soft-fail。
// 判定依据：dependabot 端点返回 400（已改用游标分页后不应再出现）；
// code_scanning 返回 403/404（Advanced Security 未启用）；
// secret_scanning 返回 404（Secret Scanning 未启用）。
func alertUnavailable(kind string, err error) bool {
	var httpErr *githubx.HTTPStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch kind {
	case store.AlertKindDependabot:
		// 游标分页改完后若仍返回 400，说明仓库不支持 Dependabot alerts。
		return httpErr.StatusCode == http.StatusBadRequest
	case store.AlertKindCodeScanning:
		// 403（Advanced Security 未启用）或 404（仓库不存在/不可见）。
		return httpErr.StatusCode == http.StatusForbidden || httpErr.StatusCode == http.StatusNotFound
	case store.AlertKindSecretScanning:
		// 404（Secret Scanning 未启用或仓库不可见）。
		return httpErr.StatusCode == http.StatusNotFound
	}
	return false
}

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ReconcileAll 对账全部自有仓（受预算限制：每轮最多 N 个）。
func (r *Reconciler) ReconcileAll(ctx context.Context, limit int) error {
	if r.OnRun != nil {
		r.OnRun()
	}
	if limit <= 0 {
		limit = 10
	}
	// 按最后同步时间升序取候选（最久未同步的优先），多取余量覆盖会被跳过的归档/不可用仓；
	// 若按更新时间倒序取仓，刚对账过的仓会永远插队，导致其余仓无限饥饿。
	repos, err := r.Store.Repositories().ListSyncCandidates(ctx, store.RepositoryTypeInstallation, limit*3)
	if err != nil {
		return err
	}
	n := 0
	for _, repo := range repos {
		if repo.SyncStatus == store.SyncStatusArchived || repo.SyncStatus == store.SyncStatusUnavailable {
			continue
		}
		if repo.IsArchived {
			// GitHub 侧已归档但本地状态未联动（历史数据）：顺手收口归档，
			// UpdateSettings 会一并关闭监控与全部能力开关。
			archived := true
			_ = r.Store.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{IsArchived: &archived})
			continue
		}
		// 监控总开关关闭的仓库不参与对账。
		if !repo.MonitorEnabled {
			continue
		}
		if err := r.ReconcileRepository(ctx, repo); err != nil && r.Logger != nil {
			r.Logger.Error("reconcile failed", "repo", repo.FullName, "error_code", "reconcile_failed")
		}
		n++
		if n >= limit {
			break
		}
	}
	return nil
}
