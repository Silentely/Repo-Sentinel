package normalizer

import (
	"context"
	"encoding/json"
	"errors"
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
	// Sender star/watch 等以顶层 sender 传达动作主体。
	Sender ghUser `json:"sender"`
	// StarredAt star 事件发生时间（载荷字段 starred_at）。
	StarredAt time.Time `json:"starred_at"`
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
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	Private         bool   `json:"private"`
	HTMLURL         string `json:"html_url"`
	DefaultBranch   string `json:"default_branch"`
	StargazersCount int64  `json:"stargazers_count"`
	Archived        bool   `json:"archived"`
	Owner           struct {
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

// ghLabel / ghMilestone 为 issue 与 PR 载荷共用的内嵌结构（字段一致，类型唯一）。
type ghLabel struct {
	Name string `json:"name"`
}

type ghMilestone struct {
	Title string `json:"title"`
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
	Labels    []ghLabel    `json:"labels"`
	Assignees []ghUser     `json:"assignees"`
	Milestone *ghMilestone `json:"milestone"`
}

type ghPullRequest struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	State     string       `json:"state"`
	HTMLURL   string       `json:"html_url"`
	User      ghUser       `json:"user"`
	UpdatedAt time.Time    `json:"updated_at"`
	Draft     bool         `json:"draft"`
	Merged    bool         `json:"merged"`
	Labels    []ghLabel    `json:"labels"`
	Assignees []ghUser     `json:"assignees"`
	Milestone *ghMilestone `json:"milestone"`
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
	case "star":
		return p.processStar(ctx, env)
	case "watch":
		return p.processWatch(ctx, env)
	case "issues":
		return p.processIssue(ctx, env)
	case "pull_request":
		return p.processPullRequest(ctx, env)
	case store.WorkflowRunKind:
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
	return NormalizeRepository(ctx, p.Store, gh, installationID)
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
		Repositories        []ghRepository `json:"repositories"`
		RepositoriesAdded   []ghRepository `json:"repositories_added"`
		RepositoriesRemoved []ghRepository `json:"repositories_removed"`
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
	// repositories_removed：仓库被移出安装（授权收回）。GitHub 侧仓库仍存在，保留历史数据
	// 但暂停采集，等待重新授权后经对账或 added 事件恢复；本地不存在的条目静默跳过。
	for i := range extra.RepositoriesRemoved {
		name := strings.TrimSpace(extra.RepositoriesRemoved[i].FullName)
		if name == "" {
			continue
		}
		if r, err := p.Store.Repositories().GetByFullName(ctx, name); err == nil {
			_ = p.Store.Repositories().UpdateSyncStatus(ctx, r.ID, store.SyncStatusUnavailable)
		}
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
		// 归档走 UpdateSettings 联动：sync_status、is_archived 与全部能力开关一起收口，
		// 与设置页手动归档的结果完全一致。
		archived := true
		_ = p.Store.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{IsArchived: &archived})
		repo.SyncStatus = store.SyncStatusArchived
		repo.IsArchived = true
	case "unarchived":
		archived := false
		_ = p.Store.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{IsArchived: &archived})
		repo.SyncStatus = store.SyncStatusActive
		repo.IsArchived = false
	case "deleted":
		// 仓库已在 GitHub 删除：本地历史数据失去上游来源，级联删除仓库与全部关联数据，
		// 确保已打开的 PR/Issue、事件、告警、快照与待投递通知不残留。
		if err := p.Store.Repositories().DeleteRepository(ctx, repo.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return Result{}, fmt.Errorf("delete repository cascade: %w", err)
		}
		// 仓库行已删除：Result 不再携带 Repository（调用方仅用于日志展示）。
		return Result{Updated: true, SuppressNotify: true}, nil
	case "transferred":
		// 转移后 App 可能失去访问权：GitHub 侧仓库仍存在，保留数据但暂停采集，
		// 等待对账 404 兜底或后续事件恢复。
		_ = p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusUnavailable)
		repo.SyncStatus = store.SyncStatusUnavailable
	case "privatized":
		repo.IsPrivate = true
		repo, _ = p.Store.Repositories().Upsert(ctx, repo)
	}
	return Result{Repository: &repo, Updated: true, SuppressNotify: true}, nil
}

// starCountMetric 快照指标名（与 store 层约定一致）。
const starCountMetric = "stargazers"

// processStar 处理 star 事件：创建事件 + 顺带写当日 star 计数快照。
// 载荷 repository.stargazers_count 为实时值，写入快照可捕捉对账间隔内的变化。
func (p *Processor) processStar(ctx context.Context, env envelope) (Result, error) {
	if env.Repository == nil {
		return Result{}, fmt.Errorf("missing repository")
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, err
	}
	if !p.ingestGate(ctx, repo, store.StarKind) {
		return Result{Repository: &repo, SuppressNotify: true}, nil
	}
	if env.Repository.StargazersCount > 0 && p.Store != nil {
		date := time.Now().UTC().Format("2006-01-02")
		if !env.StarredAt.IsZero() {
			date = env.StarredAt.UTC().Format("2006-01-02")
		}
		if _, err := p.Store.RepoStatSnapshots().Upsert(ctx, store.RepoStatSnapshot{
			RepositoryID: repo.ID, Metric: starCountMetric, Value: env.Repository.StargazersCount, SampleDate: date,
		}); err != nil {
			return Result{}, fmt.Errorf("upsert star snapshot: %w", err)
		}
	}
	actor := strings.TrimSpace(env.Sender.Login)
	if actor == "" {
		actor = "unknown"
	}
	starredAt := env.StarredAt
	if starredAt.IsZero() {
		starredAt = time.Now().UTC()
	}
	action := normalizeAction(env.Action)
	// 同秒多个用户 star 会撞指纹：StateHash 纳入 actor 与纳秒时间戳保证唯一。
	hash := StateHash(actor, starredAt.UTC().Format(time.RFC3339Nano), action)
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, store.StarKind, ResourceIdentity(store.StarKind, 0, 0), action, starredAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	title := repo.FullName
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: store.StarKind, Action: action,
		RepositoryID: &repo.ID, Title: title, Actor: actor,
		OccurredAt: starredAt, SourceUpdatedAt: &starredAt, HTMLURL: repo.HTMLURL,
		PayloadSummary:       map[string]any{"count": env.Repository.StargazersCount},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
}

// processWatch 处理 watch 事件（GitHub 仅发送 started，无取消事件）。
func (p *Processor) processWatch(ctx context.Context, env envelope) (Result, error) {
	if env.Repository == nil {
		return Result{}, fmt.Errorf("missing repository")
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, err
	}
	if !p.ingestGate(ctx, repo, store.WatchKind) {
		return Result{Repository: &repo, SuppressNotify: true}, nil
	}
	actor := strings.TrimSpace(env.Sender.Login)
	if actor == "" {
		actor = "unknown"
	}
	occurredAt := time.Now().UTC()
	action := normalizeAction(env.Action)
	hash := StateHash(actor, occurredAt.Format(time.RFC3339Nano), action)
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, store.WatchKind, ResourceIdentity(store.WatchKind, 0, 0), action, occurredAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	title := repo.FullName
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: store.WatchKind, Action: action,
		RepositoryID: &repo.ID, Title: title, Actor: actor,
		OccurredAt: occurredAt, SourceUpdatedAt: &occurredAt, HTMLURL: repo.HTMLURL,
		PayloadSummary:       map[string]any{},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
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
	issue := env.Issue
	hash := StateHash(kind, issue.State, issue.Title, issue.User.Login, milestoneTitle(issue.Milestone), strconv.FormatBool(issue.Draft))
	item := store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          issue.Number,
		Kind:            kind,
		State:           issue.State,
		Title:           issue.Title,
		Author:          issue.User.Login,
		LabelsJSON:      labelsToAny(issue.Labels),
		AssigneesJSON:   assigneesToAny(issue.Assignees),
		Milestone:       milestoneTitle(issue.Milestone),
		Draft:           issue.Draft,
		HTMLURL:         issue.HTMLURL,
		SourceUpdatedAt: issue.UpdatedAt,
		StateHash:       hash,
	}
	return p.processWorkItem(ctx, repo, kind, item, env.Action)
}

func (p *Processor) processPullRequest(ctx context.Context, env envelope) (Result, error) {
	if env.PullRequest == nil || env.Repository == nil {
		return Result{}, fmt.Errorf("missing pull_request payload")
	}
	repo, err := p.ensureRepository(ctx, env.Repository, nil)
	if err != nil {
		return Result{}, err
	}
	pr := env.PullRequest
	hash := StateHash(store.WorkItemKindPR, pr.State, pr.Title, pr.User.Login, milestoneTitle(pr.Milestone), strconv.FormatBool(pr.Draft))
	item := store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          pr.Number,
		Kind:            store.WorkItemKindPR,
		State:           pr.State,
		Title:           pr.Title,
		Author:          pr.User.Login,
		LabelsJSON:      labelsToAny(pr.Labels),
		AssigneesJSON:   assigneesToAny(pr.Assignees),
		Milestone:       milestoneTitle(pr.Milestone),
		Draft:           pr.Draft,
		Merged:          pr.Merged,
		HTMLURL:         pr.HTMLURL,
		SourceUpdatedAt: pr.UpdatedAt,
		StateHash:       hash,
	}
	res, err := p.processWorkItem(ctx, repo, store.WorkItemKindPR, item, env.Action)
	if err != nil {
		return res, err
	}
	if res.Event != nil && env.PullRequest.Merged {
		// merged 不在 StateHash 内，UpsertIfNewer 会恒早退，必须定向置位。
		if err := p.Store.WorkItems().MarkMerged(ctx, *res.Event.RepositoryID, env.PullRequest.Number); err != nil {
			return res, fmt.Errorf("mark merged: %w", err)
		}
	}
	return res, nil
}

// processWorkItem 处理 issue/PR 共用的落库与事件创建：
// 能力门禁 → UpsertIfNewer → 指纹去重 → Event 落库。kind 由调用方按载荷形态判定。
func (p *Processor) processWorkItem(ctx context.Context, repo store.Repository, kind string, item store.WorkItem, action string) (Result, error) {
	// 能力门禁：对应类型开关关闭或仓库已归档时不再采集（不更新数据、不创建事件）。
	if !p.ingestGate(ctx, repo, kind) {
		return Result{Repository: &repo, SuppressNotify: true}, nil
	}
	saved, updated, err := p.Store.WorkItems().UpsertIfNewer(ctx, item)
	if err != nil {
		return Result{}, err
	}
	if !updated {
		return Result{Repository: &repo, StaleDiscarded: true}, nil
	}
	suppress := repo.SyncStatus == store.SyncStatusBaseline || repo.SyncStatus == store.SyncStatusArchived
	fp := Fingerprint("webhook", repo.FullName, kind, ResourceIdentity(kind, saved.Number, 0), action, saved.SourceUpdatedAt, item.StateHash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	num := saved.Number
	srcUpdated := saved.SourceUpdatedAt
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: kind, Action: normalizeAction(action),
		RepositoryID: &repo.ID, SubjectNumber: &num, Title: saved.Title, Actor: saved.Author,
		OccurredAt: saved.SourceUpdatedAt, SourceUpdatedAt: &srcUpdated, HTMLURL: saved.HTMLURL,
		PayloadSummary: map[string]any{
			"state": saved.State, "draft": saved.Draft,
			"labels": item.LabelsJSON, "assignees": item.AssigneesJSON, "milestone": item.Milestone,
		},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: item.StateHash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
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
	// 能力门禁：Actions 关闭或仓库已归档时不再采集。
	if !p.ingestGate(ctx, repo, store.WorkflowRunKind) {
		return Result{Repository: &repo, SuppressNotify: true}, nil
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
	actor := strings.TrimSpace(run.Actor.Login)
	if actor == "" {
		actor = "unknown"
	}
	if strings.TrimSpace(run.HeadBranch) == "" {
		run.HeadBranch = "unknown"
	}
	if strings.TrimSpace(run.HeadSHA) == "" {
		run.HeadSHA = "0000000000000000000000000000000000000000"
	}
	if strings.TrimSpace(run.HTMLURL) == "" {
		run.HTMLURL = fmt.Sprintf("https://github.com/%s/actions/runs/%d", env.Repository.FullName, run.ID)
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
		Actor:            actor,
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
	// 恢复检测需要"上一次已完成运行"的结论作为基线，必须在本次写入之前查询，
	// 否则 LatestCompleted 会命中刚写入的当前运行，恢复事件将永不触发。
	var prevRun *store.WorkflowRun
	if run.Status == "completed" && run.Conclusion != nil && *run.Conclusion == "success" {
		if prev, err := p.Store.WorkflowRuns().LatestCompleted(ctx, repo.ID, run.WorkflowID, run.HeadBranch); err == nil {
			prevRun = &prev
		}
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
	fp := Fingerprint("webhook", repo.FullName, store.WorkflowRunKind, ResourceIdentity("workflow_run", 0, run.ID), *run.Conclusion, run.UpdatedAt, hash)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return Result{Repository: &repo, Updated: false, SuppressNotify: suppress}, nil
	}
	runID := run.ID
	srcUpdated := run.UpdatedAt
	ev := store.Event{
		ID: ulid.Make().String(), Source: "webhook", Kind: store.WorkflowRunKind, Action: "completed",
		RepositoryID: &repo.ID, Title: run.Name, Actor: actor, WorkflowRunID: &runID,
		WorkflowConclusion: *run.Conclusion, OccurredAt: run.UpdatedAt, SourceUpdatedAt: &srcUpdated,
		HTMLURL: run.HTMLURL,
		PayloadSummary: map[string]any{
			"workflow_name": run.Name, "run_number": run.RunNumber, "head_branch": run.HeadBranch,
			"head_sha": shortSHA(run.HeadSHA), "status": run.Status, "conclusion": *run.Conclusion, "attempt": run.RunAttempt,
		},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	// 恢复检测：上一次 completed 运行为失败结论时，本次成功视为恢复。
	if prevRun != nil && prevRun.GitHubRunID != run.ID && prevRun.Conclusion != nil && store.IsFailureConclusion(*prevRun.Conclusion) {
		ev.Action = "recovered"
		ev.PayloadSummary["previous_conclusion"] = *prevRun.Conclusion
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
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
	// 能力门禁：告警关闭或仓库已归档时不再采集。
	if !p.ingestGate(ctx, repo, kind) {
		return Result{Repository: &repo, SuppressNotify: true}, nil
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
		PayloadSummary:       map[string]any{"state": a.State, "severity": severity, "rule_or_dependency": rule},
		SuppressNotification: suppress, DedupeFingerprint: fp, StateHash: hash,
	}
	created, err := p.Store.Events().Create(ctx, ev)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Result{Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
		}
		return Result{}, err
	}
	return Result{Event: &created, Repository: &repo, Updated: true, SuppressNotify: suppress}, nil
}

// ingestGate 判定该仓库是否应继续采集指定类型数据。
// 顺序：全局功能开关 → 监控总开关 → 归档/不可用 → 仓库级能力开关。
// 关闭时直接跳过（不写领域数据、不创建事件）；基线同步中的仓库仍可采集（仅抑制通知）。
func (p *Processor) ingestGate(ctx context.Context, repo store.Repository, kind string) bool {
	if p.Store != nil && !store.KindFeatureEnabled(ctx, p.Store.Settings(), kind) {
		return false
	}
	return store.RepoAllowsKind(&repo, kind)
}

func normalizeAction(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return "unhandled_action"
	}
	return action
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// 以下三个转换函数供 issue/PR 共用：两类载荷的标签、指派人与里程碑结构一致，
// 避免各自重复转换逻辑。

func labelsToAny(labels []ghLabel) []any {
	out := make([]any, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.Name)
	}
	return out
}

func assigneesToAny(assignees []ghUser) []any {
	out := make([]any, 0, len(assignees))
	for _, a := range assignees {
		out = append(out, a.Login)
	}
	return out
}

func milestoneTitle(m *ghMilestone) string {
	if m == nil {
		return ""
	}
	return m.Title
}
