package normalizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Processor 将 Webhook 载荷规范化为领域状态与事件。
type Processor struct {
	Store store.Store
}

// Result 描述单次规范化结果。
type Result struct {
	Event           *store.Event
	Repository      *store.Repository
	Updated         bool
	SuppressNotify  bool
	StaleDiscarded  bool
	UnhandledAction bool
}

type envelope struct {
	Action string `json:"action"`
	// Repository may be object
	Repository *ghRepository `json:"repository"`
	// Installation
	Installation *ghInstallation `json:"installation"`
	// Issue / PR
	Issue       *ghIssue       `json:"issue"`
	PullRequest *ghPullRequest `json:"pull_request"`
	// Workflow
	WorkflowRun *ghWorkflowRun `json:"workflow_run"`
	// Alerts
	Alert *ghAlert `json:"alert"`
}

type ghRepository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type ghInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	User        ghUser    `json:"user"`
	UpdatedAt   time.Time `json:"updated_at"`
	Draft       bool      `json:"draft"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []ghUser `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

type ghPullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	User      ghUser    `json:"user"`
	UpdatedAt time.Time `json:"updated_at"`
	Draft     bool      `json:"draft"`
	Merged    bool      `json:"merged"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []ghUser `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

type ghWorkflowRun struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	WorkflowID   int64      `json:"workflow_id"`
	RunNumber    int        `json:"run_number"`
	Event        string     `json:"event"`
	Status       string     `json:"status"`
	Conclusion   *string    `json:"conclusion"`
	HTMLURL      string     `json:"html_url"`
	HeadBranch   string     `json:"head_branch"`
	HeadSHA      string     `json:"head_sha"`
	RunAttempt   int        `json:"run_attempt"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RunStartedAt *time.Time `json:"run_started_at"`
	Actor        ghUser     `json:"actor"`
}

type ghAlert struct {
	Number          int       `json:"number"`
	State           string    `json:"state"`
	HTMLURL         string    `json:"html_url"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Severity        string    `json:"severity"`
	DismissedReason string    `json:"dismissed_reason"`
	// dependabot
	Dependency *struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
	} `json:"dependency"`
	SecurityAdvisory *struct {
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
	} `json:"security_advisory"`
	// code scanning
	Rule *struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Severity    string `json:"severity"`
	} `json:"rule"`
	// secret scanning
	SecretType            string `json:"secret_type"`
	SecretTypeDisplayName string `json:"secret_type_display_name"`
}

// Process 处理已验签的 Webhook。
func (p *Processor) Process(ctx context.Context, eventType, deliveryID string, payload []byte) (Result, error) {
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return Result{}, fmt.Errorf("invalid payload: %w", err)
	}

	switch eventType {
	case "installation", "installation_repositories":
		return p.processInstallation(ctx, eventType, env, payload)
	case "repository":
		return p.processRepositoryEvent(ctx, env)
	case "issues":
		return p.processIssue(ctx, env)
	case "pull_request":
		return p.processPullRequest(ctx, env)
	case "workflow_run":
		return p.processWorkflowRun(ctx, env)
	case "dependabot_alert":
		return p.processSecurityAlert(ctx, store.AlertKindDependabot, env)
	case "code_scanning_alert":
		return p.processSecurityAlert(ctx, store.AlertKindCodeScanning, env)
	case "secret_scanning_alert":
		return p.processSecurityAlert(ctx, store.AlertKindSecretScanning, env)
	default:
		return Result{UnhandledAction: true}, nil
	}
}

func (p *Processor) ensureRepository(ctx context.Context, gh *ghRepository, installationID *string) (store.Repository, error) {
	if gh == nil || gh.FullName == "" {
		return store.Repository{}, fmt.Errorf("missing repository")
	}
	parts := strings.SplitN(gh.FullName, "/", 2)
	owner, name := gh.Owner.Login, gh.Name
	if len(parts) == 2 {
		if owner == "" {
			owner = parts[0]
		}
		if name == "" {
			name = parts[1]
		}
	}
	repoID := gh.ID
	htmlURL := strings.TrimSpace(gh.HTMLURL)
	if htmlURL == "" && gh.FullName != "" {
		htmlURL = "https://github.com/" + gh.FullName
	}
	in := store.Repository{
		Type:           store.RepositoryTypeInstallation,
		SyncStatus:     store.SyncStatusBaseline,
		GitHubRepoID:   &repoID,
		Owner:          owner,
		Name:           name,
		FullName:       gh.FullName,
		InstallationID: installationID,
		IsArchived:     gh.Archived,
		IsPrivate:      gh.Private,
		HTMLURL:        htmlURL,
		DefaultBranch:  gh.DefaultBranch,
	}
	existing, err := p.Store.Repositories().GetByFullName(ctx, gh.FullName)
	if err == nil {
		in.ID = existing.ID
		in.SyncStatus = existing.SyncStatus
		if existing.SyncStatus == "" {
			in.SyncStatus = store.SyncStatusBaseline
		}
		// 已存在仓库保持状态，仅更新元数据
		return p.Store.Repositories().Upsert(ctx, in)
	}
	if err != store.ErrNotFound {
		return store.Repository{}, err
	}
	now := time.Now().UTC()
	in.BaselineStartedAt = &now
	return p.Store.Repositories().Upsert(ctx, in)
}

func (p *Processor) processInstallation(ctx context.Context, eventType string, env envelope, payload []byte) (Result, error) {
	if env.Installation == nil {
		return Result{}, fmt.Errorf("missing installation")
	}
	inst, err := p.Store.Installations().Upsert(ctx, store.GitHubInstallation{
		InstallationID:  env.Installation.ID,
		AccountLogin:    env.Installation.Account.Login,
		AccountType:     env.Installation.Account.Type,
		TargetType:      env.Installation.Account.Type,
		PermissionsJSON: map[string]any{},
		Suspended:       "false",
	})
	if err != nil {
		return Result{}, err
	}
	instID := inst.ID
	var repo *store.Repository
	if env.Repository != nil {
		r, err := p.ensureRepository(ctx, env.Repository, &instID)
		if err != nil {
			return Result{}, err
		}
		repo = &r
	}
	// installation.created 载荷顶层为 repositories；
	// installation_repositories 的 added/removed 为 repositories_added / repositories_removed。
	var extra struct {
		Repositories      []ghRepository `json:"repositories"`
		RepositoriesAdded []ghRepository `json:"repositories_added"`
	}
	_ = json.Unmarshal(payload, &extra)
	seen := map[string]struct{}{}
	upsertList := func(list []ghRepository) error {
		for i := range list {
			name := strings.TrimSpace(list[i].FullName)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			r, err := p.ensureRepository(ctx, &list[i], &instID)
			if err != nil {
				return err
			}
			repo = &r
		}
		return nil
	}
	if err := upsertList(extra.Repositories); err != nil {
		return Result{}, err
	}
	if err := upsertList(extra.RepositoriesAdded); err != nil {
		return Result{}, err
	}
	return Result{Repository: repo, Updated: true, SuppressNotify: true}, nil
}

func (p *Processor) processRepositoryEvent(ctx context.Context, env envelope) (Result, error) {
	if env.Repository == nil {
		return Result{}, fmt.Errorf("missing repository")
	}
	var instID *string
	if env.Installation != nil {
		inst, err := p.Store.Installations().GetByInstallationID(ctx, env.Installation.ID)
		if err == nil {
			instID = &inst.ID
		}
	}
	repo, err := p.ensureRepository(ctx, env.Repository, instID)
	if err != nil {
		return Result{}, err
	}
	switch env.Action {
	case "archived":
		_ = p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusArchived)
		repo.SyncStatus = store.SyncStatusArchived
	case "unarchived":
		_ = p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusActive)
		repo.SyncStatus = store.SyncStatusActive
	case "deleted", "transferred":
		_ = p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusUnavailable)
		repo.SyncStatus = store.SyncStatusUnavailable
	case "privatized":
		repo.IsPrivate = true
		repo, _ = p.Store.Repositories().Upsert(ctx, repo)
	}
	return Result{Repository: &repo, Updated: true, SuppressNotify: true}, nil
}

func (p *Processor) processIssue(ctx context.Context, env envelope) (Result, error) {
	if env.Issue == nil || env.Repository == nil {
		return Result{}, fmt.Errorf("missing issue payload")
	}
	// PR 也可能以 issues 事件出现
	kind := store.WorkItemKindIssue
	if env.Issue.PullRequest != nil {
		kind = store.WorkItemKindPR
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, err
	}
	labels := make([]any, 0, len(env.Issue.Labels))
	for _, l := range env.Issue.Labels {
		labels = append(labels, l.Name)
	}
	assignees := make([]any, 0, len(env.Issue.Assignees))
	for _, a := range env.Issue.Assignees {
		assignees = append(assignees, a.Login)
	}
	milestone := ""
	if env.Issue.Milestone != nil {
		milestone = env.Issue.Milestone.Title
	}
	hash := StateHash(kind, env.Issue.State, env.Issue.Title, env.Issue.User.Login, milestone, strconv.FormatBool(env.Issue.Draft))
	item := store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          env.Issue.Number,
		Kind:            kind,
		State:           env.Issue.State,
		Title:           env.Issue.Title,
		Author:          env.Issue.User.Login,
		LabelsJSON:      labels,
		AssigneesJSON:   assignees,
		Milestone:       milestone,
		Draft:           env.Issue.Draft,
		HTMLURL:         env.Issue.HTMLURL,
		SourceUpdatedAt: env.Issue.UpdatedAt,
		StateHash:       hash,
	}
	saved, updated, err := p.Store.WorkItems().UpsertIfNewer(ctx, item)
	if err != nil {
		return Result{}, err
	}
	if !updated {
		return Result{Repository: &repo, StaleDiscarded: true}, nil
	}
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, kind, ResourceIdentity(kind, saved.Number, 0), env.Action, saved.SourceUpdatedAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	num := saved.Number
	srcUpdated := saved.SourceUpdatedAt
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: kind, Action: normalizeAction(env.Action),
		RepositoryID: &repo.ID, SubjectNumber: &num, Title: saved.Title, Actor: saved.Author,
		OccurredAt: saved.SourceUpdatedAt, SourceUpdatedAt: &srcUpdated, HTMLURL: saved.HTMLURL,
		PayloadSummary:       map[string]any{"state": saved.State, "draft": saved.Draft},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if err == store.ErrConflict {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
}

func (p *Processor) processPullRequest(ctx context.Context, env envelope) (Result, error) {
	if env.PullRequest == nil || env.Repository == nil {
		return Result{}, fmt.Errorf("missing pull_request payload")
	}
	// 复用 issue 结构逻辑
	env.Issue = &ghIssue{
		Number: env.PullRequest.Number, Title: env.PullRequest.Title, State: env.PullRequest.State,
		HTMLURL: env.PullRequest.HTMLURL, User: env.PullRequest.User, UpdatedAt: env.PullRequest.UpdatedAt,
		Draft: env.PullRequest.Draft, Labels: env.PullRequest.Labels, Assignees: env.PullRequest.Assignees,
		Milestone: env.PullRequest.Milestone,
		PullRequest: &struct {
			URL string `json:"url"`
		}{URL: "pr"},
	}
	res, err := p.processIssue(ctx, env)
	if err != nil {
		return res, err
	}
	if res.Event != nil && env.PullRequest.Merged {
		// 标记 merged
		item, err := p.Store.WorkItems().GetByRepoNumber(ctx, *res.Event.RepositoryID, env.PullRequest.Number)
		if err == nil {
			item.Merged = true
			item.State = "closed"
			_, _, _ = p.Store.WorkItems().UpsertIfNewer(ctx, item)
		}
	}
	return res, nil
}

func (p *Processor) processWorkflowRun(ctx context.Context, env envelope) (Result, error) {
	if env.WorkflowRun == nil || env.Repository == nil {
		return Result{}, fmt.Errorf("missing workflow_run payload")
	}
	if env.WorkflowRun.ID == 0 {
		return Result{}, fmt.Errorf("missing workflow_run id")
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, fmt.Errorf("ensure repository: %w", err)
	}
	run := env.WorkflowRun
	conclusion := ""
	if run.Conclusion != nil {
		conclusion = *run.Conclusion
	}
	// GitHub 偶发缺字段；入库前补默认值，避免 Ent 必填校验失败。
	if run.RunAttempt <= 0 {
		run.RunAttempt = 1
	}
	if strings.TrimSpace(run.Status) == "" {
		run.Status = "unknown"
	}
	if run.UpdatedAt.IsZero() {
		if !run.CreatedAt.IsZero() {
			run.UpdatedAt = run.CreatedAt
		} else {
			run.UpdatedAt = time.Now().UTC()
		}
	}
	name := strings.TrimSpace(run.Name)
	if name == "" {
		name = "workflow"
	}
	hash := StateHash(strconv.FormatInt(run.ID, 10), run.Status, conclusion, strconv.Itoa(run.RunAttempt), run.HeadSHA)
	in := store.WorkflowRun{
		RepositoryID:     repo.ID,
		GitHubRunID:      run.ID,
		GitHubWorkflowID: run.WorkflowID,
		WorkflowName:     name,
		RunNumber:        run.RunNumber,
		Event:            run.Event,
		HeadBranch:       run.HeadBranch,
		HeadSHA:          run.HeadSHA,
		Status:           run.Status,
		Conclusion:       run.Conclusion,
		Actor:            run.Actor.Login,
		RunAttempt:       run.RunAttempt,
		HTMLURL:          run.HTMLURL,
		RunStartedAt:     run.RunStartedAt,
		RunUpdatedAt:     run.UpdatedAt,
		StateHash:        hash,
	}
	if run.Status == "completed" {
		t := run.UpdatedAt
		in.RunCompletedAt = &t
	}
	_, updated, err := p.Store.WorkflowRuns().UpsertIfNewer(ctx, in)
	if err != nil {
		return Result{}, fmt.Errorf("upsert workflow_run: %w", err)
	}
	if !updated {
		return Result{Repository: &repo, StaleDiscarded: true}, nil
	}
	// 仅 completed 产生业务事件
	if run.Status != "completed" || run.Conclusion == nil {
		return Result{Repository: &repo, Updated: true, SuppressNotify: true}, nil
	}
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, "workflow_run", ResourceIdentity("workflow_run", 0, run.ID), *run.Conclusion, run.UpdatedAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	runID := run.ID
	srcUpdated := run.UpdatedAt
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: "workflow_run", Action: "completed",
		RepositoryID: &repo.ID, Title: run.Name, Actor: run.Actor.Login, WorkflowRunID: &runID,
		WorkflowConclusion: *run.Conclusion, OccurredAt: run.UpdatedAt, SourceUpdatedAt: &srcUpdated,
		HTMLURL: run.HTMLURL,
		PayloadSummary: map[string]any{
			"run_number": run.RunNumber, "head_branch": run.HeadBranch, "head_sha": shortSHA(run.HeadSHA),
			"status": run.Status, "conclusion": *run.Conclusion, "attempt": run.RunAttempt,
		},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	// 恢复检测
	if *run.Conclusion == "success" {
		if prev, err := p.Store.WorkflowRuns().LatestCompleted(ctx, repo.ID, run.WorkflowID, run.HeadBranch); err == nil {
			if prev.GitHubRunID != run.ID && prev.Conclusion != nil && isFailureConclusion(*prev.Conclusion) {
				ev.Action = "recovered"
				ev.PayloadSummary["previous_conclusion"] = *prev.Conclusion
			}
		}
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if err == store.ErrConflict {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
}

func (p *Processor) processSecurityAlert(ctx context.Context, kind string, env envelope) (Result, error) {
	if env.Alert == nil || env.Repository == nil {
		return Result{}, fmt.Errorf("missing alert payload")
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, err
	}
	a := env.Alert
	severity := a.Severity
	rule := ""
	switch kind {
	case store.AlertKindDependabot:
		if a.SecurityAdvisory != nil {
			if severity == "" {
				severity = a.SecurityAdvisory.Severity
			}
			rule = a.SecurityAdvisory.Summary
		}
		if a.Dependency != nil {
			rule = a.Dependency.Package.Name
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
		// 永不保存 secret 值
	}
	updatedAt := a.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = a.CreatedAt
	}
	hash := StateHash(kind, a.State, severity, rule, a.DismissedReason)
	in := store.SecurityAlert{
		RepositoryID: repo.ID, AlertKind: kind, AlertNumber: a.Number, State: a.State,
		Severity: severity, RuleOrDependency: rule, DismissedReason: a.DismissedReason,
		HTMLURL: a.HTMLURL, SourceUpdatedAt: updatedAt, StateHash: hash,
	}
	_, updated, err := p.Store.SecurityAlerts().UpsertIfNewer(ctx, in)
	if err != nil {
		return Result{}, err
	}
	if !updated {
		return Result{Repository: &repo, StaleDiscarded: true}, nil
	}
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, kind, ResourceIdentity(kind, a.Number, 0), env.Action, updatedAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	num := a.Number
	srcUpdated := updatedAt
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: kind, Action: normalizeAction(env.Action),
		RepositoryID: &repo.ID, SubjectNumber: &num, Title: rule, Severity: severity,
		OccurredAt: updatedAt, SourceUpdatedAt: &srcUpdated, HTMLURL: a.HTMLURL,
		PayloadSummary:       map[string]any{"state": a.State, "severity": severity},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if err == store.ErrConflict {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
}

func normalizeAction(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return "unhandled_action"
	}
	return action
}

func isFailureConclusion(c string) bool {
	switch c {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return true
	default:
		return false
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
