package syncx

import (
	"context"
	"fmt"
	"log/slog"
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
		_ = r.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, repo.SyncStatus)
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

	if err := r.syncIssues(ctx, token, repo, since, isBaseline); err != nil {
		return err
	}
	if err := r.syncWorkflows(ctx, token, repo, isBaseline); err != nil {
		// Actions 权限不足时软失败
		if r.Logger != nil {
			r.Logger.Warn("workflow reconcile soft-fail", "repo", repo.FullName, "error_code", "reconcile_workflows")
		}
	}
	_ = r.syncAlerts(ctx, token, repo, store.AlertKindDependabot, isBaseline)
	_ = r.syncAlerts(ctx, token, repo, store.AlertKindCodeScanning, isBaseline)
	_ = r.syncAlerts(ctx, token, repo, store.AlertKindSecretScanning, isBaseline)

	now := time.Now().UTC()
	repo.LastSyncedAt = &now
	repo.LastSyncErrorCode = ""
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

func (r *Reconciler) syncIssues(ctx context.Context, token string, repo store.Repository, since *time.Time, baseline bool) error {
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
	var (
		items []githubx.AlertItem
		err   error
	)
	switch kind {
	case store.AlertKindDependabot:
		items, _, err = r.GitHub.ListDependabotAlerts(ctx, token, repo.Owner, repo.Name, 1)
	case store.AlertKindCodeScanning:
		items, _, err = r.GitHub.ListCodeScanningAlerts(ctx, token, repo.Owner, repo.Name, 1)
	case store.AlertKindSecretScanning:
		items, _, err = r.GitHub.ListSecretScanningAlerts(ctx, token, repo.Owner, repo.Name, 1)
	}
	if err != nil {
		return err
	}
	for _, a := range items {
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
		_ = baseline
	}
	return nil
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
	repos, _, err := r.Store.Repositories().List(ctx, store.ListFilter{Page: 1, PerPage: 100, Kind: store.RepositoryTypeInstallation})
	if err != nil {
		return err
	}
	n := 0
	for _, repo := range repos {
		if repo.SyncStatus == store.SyncStatusArchived || repo.SyncStatus == store.SyncStatusUnavailable {
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
