package store

import (
	"context"
	"encoding/json"
	"time"
)

// 仓库类型与同步状态常量。
const (
	RepositoryTypeInstallation = "github_installation"
	RepositoryTypeExternal     = "external_public"

	SyncStatusBaseline    = "baseline_sync"
	SyncStatusActive      = "active"
	SyncStatusArchived    = "archived"
	SyncStatusUnavailable = "unavailable"

	WorkItemKindIssue = "issue"
	WorkItemKindPR    = "pull_request"
	// WorkflowRunKind Actions 工作流运行的事件类型（此前全项目散落裸字符串，收敛为常量防拼写漂移）。
	WorkflowRunKind = "workflow_run"

	// StarKind / WatchKind 仓库影响力事件类型。
	StarKind  = "star"
	WatchKind = "watch"

	// ReleaseKind star 仓库的 GitHub Release 发布事件类型。
	ReleaseKind = "release"

	// MetricStargazers 快照指标名（与 normalizer 包 starCountMetric 常量字符串一致，
	// 两处固定为 "stargazers"；store 聚合、syncx 对账与 normalizer 写入共用该值）。
	MetricStargazers = "stargazers"

	AlertKindDependabot     = "dependabot"
	AlertKindCodeScanning   = "code_scanning"
	AlertKindSecretScanning = "secret_scanning"

	// AlertStateWithdrawn 安全告警源端撤回状态：GitHub 不推送该状态、也不会再出现在
	// 告警列表 API 中，由对账差集检测写入本地，避免「源端已消失、本地永远待处理」。
	AlertStateWithdrawn = "withdrawn"

	ChannelTelegram    = "telegram"
	ChannelHTTPWebhook = "http_webhook"

	OutboxPending = "pending"
	OutboxSending = "sending"
	OutboxSent    = "sent"
	OutboxDead    = "dead"

	DeliveryAccepted  = "accepted"
	DeliveryProcessed = "processed"
	DeliveryFailed    = "failed"
	DeliveryDuplicate = "duplicate"
)

// GitHubInstallation 领域模型。
type GitHubInstallation struct {
	ID              string         `json:"id"`
	InstallationID  int64          `json:"installation_id"`
	AccountLogin    string         `json:"account_login"`
	AccountType     string         `json:"account_type"`
	TargetType      string         `json:"target_type"`
	PermissionsJSON map[string]any `json:"permissions,omitempty"`
	Suspended       string         `json:"suspended"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// Repository 领域模型。
type Repository struct {
	ID                 string     `json:"id"`
	Type               string     `json:"type"`
	SyncStatus         string     `json:"sync_status"`
	GitHubRepoID       *int64     `json:"github_repo_id,omitempty"`
	Owner              string     `json:"owner"`
	Name               string     `json:"name"`
	FullName           string     `json:"full_name"`
	InstallationID     *string    `json:"installation_id,omitempty"`
	IsArchived         bool       `json:"is_archived"`
	IsPrivate          bool       `json:"is_private"`
	MonitorEnabled     bool       `json:"monitor_enabled"`
	IssuesEnabled      bool       `json:"issues_enabled"`
	PrEnabled          bool       `json:"pr_enabled"`
	ActionsEnabled     bool       `json:"actions_enabled"`
	AlertsEnabled      bool       `json:"alerts_enabled"`
	StarsEnabled       bool       `json:"stars_enabled"`
	WatchesEnabled     bool       `json:"watches_enabled"`
	HTMLURL            string     `json:"html_url"`
	DefaultBranch      string     `json:"default_branch"`
	BaselineStartedAt  *time.Time `json:"baseline_started_at,omitempty"`
	BaselineFinishedAt *time.Time `json:"baseline_finished_at,omitempty"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	LastSyncErrorCode  string     `json:"last_sync_error_code,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// RepoAllowsKind 判定仓库级能力开关是否放行该类型的采集/通知。
// repo 为 nil 时放行（聚合器 flush 单事件回放等场景无仓库上下文，仅依赖全局开关）。
// normalizer（采集门禁）与 rules（通知门禁）共用此实现，避免两份逐字重复后行为漂移。
func RepoAllowsKind(repo *Repository, kind string) bool {
	if repo == nil {
		return true
	}
	if !repo.MonitorEnabled {
		return false
	}
	if repo.IsArchived || repo.SyncStatus == SyncStatusArchived || repo.SyncStatus == SyncStatusUnavailable {
		return false
	}
	switch kind {
	case WorkItemKindIssue:
		return repo.IssuesEnabled
	case WorkItemKindPR:
		return repo.PrEnabled
	case WorkflowRunKind:
		return repo.ActionsEnabled
	case AlertKindDependabot, AlertKindCodeScanning, AlertKindSecretScanning:
		return repo.AlertsEnabled
	case StarKind:
		return repo.StarsEnabled
	case WatchKind:
		return repo.WatchesEnabled
	}
	return true
}

// GitHubViewLabel 通知正文与 Telegram inline keyboard 的 GitHub 跳转按钮文案。
// 收敛到领域层单一来源：engine（HTML 链接）与 notify（inline button）共用，避免两处漂移。
const GitHubViewLabel = "🔗 在 GitHub 中查看"

// KindDisplayName 事件类型中文/友好名（推送正文与 AI 分诊输入用，避免 raw kind）。
func KindDisplayName(kind string) string {
	switch kind {
	case WorkItemKindIssue:
		return "Issue"
	case WorkItemKindPR:
		return "PR"
	case AlertKindDependabot:
		return "Dependabot 依赖告警"
	case AlertKindCodeScanning:
		return "Code Scanning 代码扫描"
	case AlertKindSecretScanning:
		return "Secret Scanning 密钥扫描"
	case WorkflowRunKind:
		return "Actions 工作流"
	case StarKind:
		return "Star"
	case WatchKind:
		return "Watch"
	case ReleaseKind:
		return "Release"
	default:
		return kind
	}
}

// PayloadString 从 PayloadSummary 安全读取字符串字段。
func PayloadString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// EventRepoName 解析事件所属仓库名，供报告预览与 AI 总结引用。
// 优先 RepositoryID → repositories 映射；star 追踪的 release 事件没有 RepositoryID
// （外部 star 仓不建 Repository 行），回退 PayloadSummary["repository"]（syncx 写入）。
func EventRepoName(ev Event, repoNames map[string]string) string {
	if ev.RepositoryID != nil && repoNames != nil {
		if name := repoNames[*ev.RepositoryID]; name != "" {
			return name
		}
	}
	return PayloadString(ev.PayloadSummary, "repository")
}

// CoerceInt 将 JSON 数值（float64/int/int64/json.Number）收敛为整数；
// 非数值或小数（如 3.5）返回 false。保留天数、聚合窗口等设置解析共用此实现，
// 避免各包自行转换导致边界行为漂移。
func CoerceInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// NormalizeListFilter 归一化分页参数：缺省页号 1、每页 20，上限 100。
// 列表查询与 HTTP 早退分支共用，保证两处分页语义一致。
func NormalizeListFilter(f ListFilter) ListFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 {
		f.PerPage = 20
	}
	if f.PerPage > 100 {
		f.PerPage = 100
	}
	// 页号上限钳制：Offset=(Page-1)*PerPage 在 Page 极大时可能整数溢出为负，
	// 公开 API 的 page 参数不可信，钳到合理上限后查询退化到最后一页附近。
	if f.Page > 100_000 {
		f.Page = 100_000
	}
	return f
}

// WebhookDelivery 领域模型。
type WebhookDelivery struct {
	ID                 string     `json:"id"`
	DeliveryID         string     `json:"delivery_id"`
	EventType          string     `json:"event_type"`
	Action             string     `json:"action,omitempty"`
	RepositoryFullName string     `json:"repository_full_name,omitempty"`
	Status             string     `json:"status"`
	ErrorCode          string     `json:"error_code,omitempty"`
	Payload            []byte     `json:"-"`
	ReceivedAt         time.Time  `json:"received_at"`
	ProcessedAt        *time.Time `json:"processed_at,omitempty"`
}

// WorkItem 领域模型。
type WorkItem struct {
	ID                 string    `json:"id"`
	RepositoryID       string    `json:"repository_id"`
	RepositoryFullName string    `json:"repository_full_name,omitempty"`
	Number             int       `json:"number"`
	Kind               string    `json:"kind"`
	State              string    `json:"state"`
	Title              string    `json:"title"`
	Author             string    `json:"author"`
	LabelsJSON         []any     `json:"labels,omitempty"`
	AssigneesJSON      []any     `json:"assignees,omitempty"`
	Milestone          string    `json:"milestone,omitempty"`
	Draft              bool      `json:"draft"`
	Merged             bool      `json:"merged"`
	HTMLURL            string    `json:"html_url"`
	SourceUpdatedAt    time.Time `json:"source_updated_at"`
	StateHash          string    `json:"state_hash,omitempty"`
	// 新增 Review 相关字段
	ReviewState    string   `json:"review_state,omitempty"`    // 最新审核状态：APPROVED, CHANGES_REQUESTED, COMMENTED, PENDING
	ReviewDecision string   `json:"review_decision,omitempty"` // 审核决策：approved, changes_requested
	Reviewers      []string `json:"reviewers,omitempty"`       // 已请求审核的人
	// 新增 Check Runs 相关字段
	CheckStatus     string `json:"check_status,omitempty"`     // 检查状态：success, failure, pending
	CheckConclusion string `json:"check_conclusion,omitempty"` // 检查结论
	ChecksTotal     int    `json:"checks_total,omitempty"`     // 总检查数
	ChecksPassed    int    `json:"checks_passed,omitempty"`    // 通过检查数
	// Ignored 为本地忽略标记，不回写 GitHub。
	Ignored   bool      `json:"ignored"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkflowRun 领域模型。
type WorkflowRun struct {
	ID                 string     `json:"id"`
	RepositoryID       string     `json:"repository_id"`
	RepositoryFullName string     `json:"repository_full_name,omitempty"`
	GitHubRunID        int64      `json:"github_run_id"`
	GitHubWorkflowID   int64      `json:"github_workflow_id"`
	WorkflowName       string     `json:"workflow_name"`
	RunNumber          int        `json:"run_number"`
	Event              string     `json:"event"`
	HeadBranch         string     `json:"head_branch"`
	HeadSHA            string     `json:"head_sha"`
	Status             string     `json:"status"`
	Conclusion         *string    `json:"conclusion,omitempty"`
	PreviousConclusion *string    `json:"previous_conclusion,omitempty"`
	Actor              string     `json:"actor"`
	RunAttempt         int        `json:"run_attempt"`
	HTMLURL            string     `json:"html_url"`
	RunStartedAt       *time.Time `json:"run_started_at,omitempty"`
	RunUpdatedAt       time.Time  `json:"run_updated_at"`
	RunCompletedAt     *time.Time `json:"run_completed_at,omitempty"`
	StateHash          string     `json:"state_hash,omitempty"`
	Ignored            bool       `json:"ignored"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// SecurityAlert 领域模型。
type SecurityAlert struct {
	ID                 string    `json:"id"`
	RepositoryID       string    `json:"repository_id"`
	RepositoryFullName string    `json:"repository_full_name,omitempty"`
	AlertKind          string    `json:"alert_kind"`
	AlertNumber        int       `json:"alert_number"`
	State              string    `json:"state"`
	Severity           string    `json:"severity"`
	RuleOrDependency   string    `json:"rule_or_dependency"`
	DismissedReason    string    `json:"dismissed_reason,omitempty"`
	HTMLURL            string    `json:"html_url"`
	SourceUpdatedAt    time.Time `json:"source_updated_at"`
	StateHash          string    `json:"state_hash,omitempty"`
	Ignored            bool      `json:"ignored"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Event 规范化业务事件。
type Event struct {
	ID                   string         `json:"id"`
	Source               string         `json:"source"`
	Kind                 string         `json:"kind"`
	Action               string         `json:"action"`
	RepositoryID         *string        `json:"repository_id,omitempty"`
	SubjectNumber        *int64         `json:"subject_number,omitempty"`
	Title                string         `json:"title"`
	Severity             string         `json:"severity"`
	Actor                string         `json:"actor"`
	WorkflowRunID        *int64         `json:"workflow_run_id,omitempty"`
	WorkflowConclusion   string         `json:"workflow_conclusion,omitempty"`
	OccurredAt           time.Time      `json:"occurred_at"`
	SourceUpdatedAt      *time.Time     `json:"source_updated_at,omitempty"`
	HTMLURL              string         `json:"html_url"`
	PayloadSummary       map[string]any `json:"payload_summary,omitempty"`
	SuppressNotification bool           `json:"suppress_notification"`
	DedupeFingerprint    string         `json:"dedupe_fingerprint,omitempty"`
	StateHash            string         `json:"state_hash,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
}

// RepoStatSnapshot 仓库指标按天快照（当前仅 stargazers，metric 预留扩展）。
type RepoStatSnapshot struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	Metric       string    `json:"metric"`
	Value        int64     `json:"value"`
	SampleDate   string    `json:"sample_date"` // UTC 日期 YYYY-MM-DD
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StarTrendPoint 仪表盘 star 趋势序列点。
type StarTrendPoint struct {
	Date  string `json:"date"`
	Total int64  `json:"total"`
}

// NotificationChannel 通知渠道。
type NotificationChannel struct {
	ID             string `json:"id"`
	ChannelType    string `json:"channel_type"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Target         string `json:"target"`
	SecretEnvelope string `json:"-"`
	AllowPrivate   bool   `json:"allow_private"`
	// EventKinds 订阅的实时通知类型；nil 表示全部，空数组表示不订阅实时通知。
	EventKinds []string `json:"event_kinds"`
	// DigestEnabled 是否接收每日汇总。
	DigestEnabled bool      `json:"digest_enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// subscribableKinds 渠道可订阅的实时通知类型白名单（不可变，禁止外部修改）。
var subscribableKinds = map[string]struct{}{
	WorkItemKindIssue:       {},
	WorkItemKindPR:          {},
	WorkflowRunKind:         {},
	AlertKindDependabot:     {},
	AlertKindCodeScanning:   {},
	AlertKindSecretScanning: {},
	StarKind:                {},
	WatchKind:               {},
	ReleaseKind:             {},
}

// IsSubscribableKind 判定 kind 是否在订阅白名单内。
func IsSubscribableKind(kind string) bool {
	_, ok := subscribableKinds[kind]
	return ok
}

// AcceptsKind 渠道是否接收该类型的实时通知；EventKinds 为 nil 表示全部订阅。
func (c NotificationChannel) AcceptsKind(kind string) bool {
	if c.EventKinds == nil {
		return true
	}
	for _, k := range c.EventKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// NotificationOutbox 待投递通知。
type NotificationOutbox struct {
	ID             string         `json:"id"`
	ChannelID      string         `json:"channel_id"`
	EventID        *string        `json:"event_id,omitempty"`
	AggregateKey   string         `json:"aggregate_key,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Status         string         `json:"status"`
	AttemptCount   int            `json:"attempt_count"`
	NextAttemptAt  time.Time      `json:"next_attempt_at"`
	LockedUntil    *time.Time     `json:"locked_until,omitempty"`
	LastErrorCode  string         `json:"last_error_code,omitempty"`
	Title          string         `json:"title"`
	BodyText       string         `json:"body_text,omitempty"`
	BodyJSON       map[string]any `json:"body_json,omitempty"`
	ParseMode      string         `json:"parse_mode,omitempty"`
	HTMLURL        string         `json:"html_url,omitempty"` // Telegram inline keyboard 跳转链接
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// MaxExternalRepositories 外部公开仓库上限（同一产品规则，写入与文案共用）。
const MaxExternalRepositories = 20

// SyncCursor 同步游标。
type SyncCursor struct {
	ID            string
	RepositoryID  string
	Resource      string
	CursorValue   string
	ETag          string
	LastSuccessAt *time.Time
	LastErrorCode string
	UpdatedAt     time.Time
}

// StarredRepoTracker 状态常量。
const (
	TrackerStateTracking    = "tracking"    // 正常轮询 release
	TrackerStateInactive    = "inactive"    // 从未发布 release，7 天复查
	TrackerStateDisabled    = "disabled"    // 用户停用或用户 unstar，保留记录
	TrackerStateUnavailable = "unavailable" // 404/410 删仓或转私有
)

// StarredRepoTracker 用户 star 仓库的 release 追踪记录。
type StarredRepoTracker struct {
	ID                     string
	FullName               string
	State                  string
	ETag                   string
	LastReleaseID          int64
	LastReleaseTag         string
	LastReleasePublishedAt *time.Time
	NoReleaseSince         *time.Time
	NoReleaseRecheckAt     *time.Time
	FirstSeenAt            time.Time
	LastPollAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// ListFilter 通用列表筛选。
type ListFilter struct {
	Page    int
	PerPage int
	// 可选筛选
	RepositoryID string
	Kind         string
	State        string
	Status       string
	// ChannelIDs 限定 Outbox 的渠道集合（SQL 层过滤，保证分页与 total 正确）。
	ChannelIDs []string
	// ReviewDecision 按 PR 审核结论过滤（approved / changes_requested）。
	ReviewDecision string
	// CheckStatus 按 PR 检查状态过滤：success / failure；空串值 "pending" 表示尚无检查数据。
	CheckStatus string
	// IncludeIgnored=true 时包含已忽略项；默认 false 只返回未忽略。
	IncludeIgnored bool
	// OnlyIgnored=true 时仅返回已忽略项（优先于 IncludeIgnored）。
	OnlyIgnored bool
	// IncludeArchivedRepos=true 时包含已归档仓库的数据；默认 false 排除。
	IncludeArchivedRepos bool
}

// PageResult 分页结果元数据。
type PageResult struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// RepositoryStore 仓库持久化。
type RepositoryStore interface {
	Upsert(context.Context, Repository) (Repository, error)
	Get(context.Context, string) (Repository, error)
	GetByFullName(context.Context, string) (Repository, error)
	GetByGitHubRepoID(context.Context, int64) (Repository, error)
	List(context.Context, ListFilter) ([]Repository, PageResult, error)
	// ListSyncCandidates 按最后同步时间升序返回指定类型的仓库（从未同步的排最前）。
	// 对账/轮询调度专用：按 updated_at 取仓会让刚同步过的仓永远插队，导致其余仓饥饿。
	ListSyncCandidates(ctx context.Context, repoType string, limit int) ([]Repository, error)
	UpdateSyncStatus(context.Context, string, string) error
	UpdateSettings(context.Context, string, RepositorySettings) error
	CountByType(context.Context, string) (int, error)
	// DeleteRepository 级联删除仓库及其全部关联数据（work_items、workflow_runs、security_alerts、
	// events 及引用这些事件的 outbox、sync_cursors、repo_stat_snapshots），整个删除在一个事务内完成。
	// 用于 GitHub 侧仓库已删除（webhook repository.deleted）时的本地数据清理；
	// 仓库不存在时返回 ErrNotFound，调用方可按幂等语义忽略。
	DeleteRepository(context.Context, string) error
}

// RepositorySettings 仓库能力开关与归档设置。
type RepositorySettings struct {
	MonitorEnabled *bool `json:"monitor_enabled,omitempty"`
	IssuesEnabled  *bool `json:"issues_enabled,omitempty"`
	PrEnabled      *bool `json:"pr_enabled,omitempty"`
	ActionsEnabled *bool `json:"actions_enabled,omitempty"`
	AlertsEnabled  *bool `json:"alerts_enabled,omitempty"`
	StarsEnabled   *bool `json:"stars_enabled,omitempty"`
	WatchesEnabled *bool `json:"watches_enabled,omitempty"`
	IsArchived     *bool `json:"is_archived,omitempty"`
}

// InstallationStore GitHub App 安装。
type InstallationStore interface {
	Upsert(context.Context, GitHubInstallation) (GitHubInstallation, error)
	Get(context.Context, string) (GitHubInstallation, error)
	GetByInstallationID(context.Context, int64) (GitHubInstallation, error)
	List(context.Context) ([]GitHubInstallation, error)
}

// WebhookDeliveryStore delivery 去重。
type WebhookDeliveryStore interface {
	Create(context.Context, WebhookDelivery) (WebhookDelivery, error)
	GetByDeliveryID(context.Context, string) (WebhookDelivery, error)
	MarkProcessed(context.Context, string, string, string) error
	// DeleteOlderThan 删除 received_at 早于 cutoff 的 delivery 记录，返回删除行数。
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// WorkItemStore Issue/PR。
type WorkItemStore interface {
	GetByRepoNumber(context.Context, string, int) (WorkItem, error)
	Get(context.Context, string) (WorkItem, error)
	UpsertIfNewer(context.Context, WorkItem) (WorkItem, bool, error) // bool=updated
	List(context.Context, ListFilter) ([]WorkItem, PageResult, error)
	CountOpen(context.Context) (int, error)
	SetIgnored(context.Context, string, bool) error
	// MarkMerged 定向置位 PR 合并标记；StateHash 不含 merged，不能走 UpsertIfNewer。
	MarkMerged(context.Context, string, int) error
}

// WorkflowRunStore Actions。
type WorkflowRunStore interface {
	GetByRepoRunID(context.Context, string, int64) (WorkflowRun, error)
	Get(context.Context, string) (WorkflowRun, error)
	UpsertIfNewer(context.Context, WorkflowRun) (WorkflowRun, bool, error)
	LatestCompleted(context.Context, string, int64, string) (WorkflowRun, error)
	List(context.Context, ListFilter) ([]WorkflowRun, PageResult, error)
	CountFailed(context.Context) (int, error)
	SetIgnored(context.Context, string, bool) error
}

// SecurityAlertStore 安全告警。
type SecurityAlertStore interface {
	GetByIdentity(context.Context, string, string, int) (SecurityAlert, error)
	Get(context.Context, string) (SecurityAlert, error)
	UpsertIfNewer(context.Context, SecurityAlert) (SecurityAlert, bool, error)
	List(context.Context, ListFilter) ([]SecurityAlert, PageResult, error)
	// ListByRepoKind 返回指定仓库与类型的全部本地告警（不分状态），供对账差集判定。
	ListByRepoKind(context.Context, string, string) ([]SecurityAlert, error)
	CountOpen(context.Context) (int, error)
	SetIgnored(context.Context, string, bool) error
}

// EventStore 业务事件。
type EventStore interface {
	Create(context.Context, Event) (Event, error)
	GetByFingerprint(context.Context, string) (Event, error)
	List(context.Context, ListFilter) ([]Event, PageResult, error)
	CountSince(context.Context, time.Time) (int, error)
	// ListSince 查询指定时间之后的事件，按时间倒序，limit 控制最大返回数。
	// 每日摘要专用：已归档仓库与被抑制（基线/归档期间产生）的事件不返回。
	ListSince(ctx context.Context, since time.Time, limit int) ([]Event, error)
	// DeleteOlderThan 删除 created_at 早于 cutoff 的事件，返回删除行数。
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// RepoStatSnapshotStore 仓库指标快照。
// ListInRange 参数：repoIDs、metric、fromDate、toDate（均为 YYYY-MM-DD，含边界）。
type RepoStatSnapshotStore interface {
	Upsert(context.Context, RepoStatSnapshot) (RepoStatSnapshot, error)
	ListInRange(context.Context, []string, string, string, string) ([]RepoStatSnapshot, error)
}

// StarredTrackerStore 用户 star 仓库的 release 追踪记录。
type StarredTrackerStore interface {
	// Upsert 按 full_name 幂等写入：已存在则更新，否则创建。
	Upsert(context.Context, StarredRepoTracker) error
	GetByFullName(context.Context, string) (StarredRepoTracker, error)
	// ListAll 返回全部追踪记录（不分状态），供 star 同步一次性建 full_name→tracker
	// 映射，避免逐仓 GetByFullName 的 N+1 查询；limit 截断（配合追踪上限配置）。
	ListAll(context.Context, int) ([]StarredRepoTracker, error)
	// ListPollCandidates 返回 state=tracking 的候选，按 last_poll_at 升序（未轮询过优先），limit 截断。
	ListPollCandidates(context.Context, int) ([]StarredRepoTracker, error)
	// UpdatePollResult 推进 release 轮询结果（etag 与最新 release 信息），并更新 last_poll_at。
	UpdatePollResult(context.Context, string, string, int64, string, *time.Time) error
	// UpdateNoRelease 标记仓库无 release（进入 inactive），并设置下次复查时间。
	UpdateNoRelease(context.Context, string, time.Time) error
	UpdateState(context.Context, string, string) error
	// CountByState 按 state 统计数量，供管理台状态概览。
	CountByState(context.Context) (map[string]int, error)
	List(context.Context, ListFilter) ([]StarredRepoTracker, PageResult, error)
}

// ChannelStore 通知渠道。
type ChannelStore interface {
	Upsert(context.Context, NotificationChannel) (NotificationChannel, error)
	Get(context.Context, string) (NotificationChannel, error)
	GetEnabledByType(context.Context, string) (NotificationChannel, error)
	List(context.Context) ([]NotificationChannel, error)
	DisableOthersOfType(context.Context, string, string) error
	Delete(context.Context, string) error
	ToggleEnabled(context.Context, string, bool) error
}

// OutboxStore 通知 Outbox。
type OutboxStore interface {
	Create(context.Context, NotificationOutbox) (NotificationOutbox, error)
	ClaimDue(context.Context, time.Time, time.Duration, int) ([]NotificationOutbox, error)
	MarkSent(context.Context, string) error
	MarkRetry(context.Context, string, time.Time, string) error
	MarkDead(context.Context, string, string) error
	List(context.Context, ListFilter) ([]NotificationOutbox, PageResult, error)
	CountByStatus(context.Context, string) (int, error)
	RetryDead(context.Context, string, time.Time) error
	// RetryAllDead 将全部（或 channelIDs 限定渠道）dead 投递重新排队，返回重新排队条数；
	// channelIDs 为空切片表示不过滤渠道。单次 UPDATE 完成，与逐条 RetryDead 语义一致。
	RetryAllDead(context.Context, []string, time.Time) (int, error)
	// DeleteTerminalOlderThan 删除已终态（sent/dead）且 created_at 早于 cutoff 的投递记录。
	DeleteTerminalOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// RetentionPolicy 历史数据保留策略（天）。某字段为 0 表示跳过该类清理。
type RetentionPolicy struct {
	EventsDays            int
	OutboxDays            int
	WebhookDeliveriesDays int
}

// CleanupResult 一次保留清理删除的行数。
type CleanupResult struct {
	EventsDeleted            int `json:"events_deleted"`
	OutboxDeleted            int `json:"outbox_deleted"`
	WebhookDeliveriesDeleted int `json:"webhook_deliveries_deleted"`
}

// DefaultRetentionPolicy 返回管理台默认保留天数。
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		EventsDays:            90,
		OutboxDays:            30,
		WebhookDeliveriesDays: 30,
	}
}

// CursorStore 同步游标。
type CursorStore interface {
	Get(context.Context, string, string) (SyncCursor, error)
	Upsert(context.Context, SyncCursor) (SyncCursor, error)
}

// DashboardStats 仪表盘聚合。
type DashboardStats struct {
	OpenIssues      int `json:"open_issues"`
	OpenPulls       int `json:"open_pulls"`
	FailedActions   int `json:"failed_actions"`
	OpenSecurity    int `json:"open_security"`
	Events24h       int `json:"events_24h"`
	OutboxDead      int `json:"outbox_dead"`
	ReposActive     int `json:"repos_active"`
	ReposBaseline   int `json:"repos_baseline"`
	ChannelsEnabled int `json:"channels_enabled"`
}

// failedConclusionSet 视为失败的 Workflow 结论集合（不可变，禁止外部修改）。
// 恢复检测（normalizer）、实时通知（rules）与仪表盘计数（Dashboard/CountFailed）共用此判定，
// 任何一处漏改都会导致行为不一致，故收敛为单一来源。
var failedConclusionSet = map[string]struct{}{
	"failure":         {},
	"timed_out":       {},
	"cancelled":       {},
	"action_required": {},
	"startup_failure": {},
}

// IsFailureConclusion 判定 Workflow 结论是否属于失败类。
func IsFailureConclusion(c string) bool {
	_, ok := failedConclusionSet[c]
	return ok
}

// WorkflowConclusionLabel 返回 Workflow 结论的中文标签（成功/失败/已取消/…）。
// rules 实时通知与 digest 定期报告共用同一映射，避免两处维护漂移；
// 前端同义文案见 web/src/lib/format.ts 的 workflowConclusionLabel。
func WorkflowConclusionLabel(conclusion string) string {
	switch conclusion {
	case "success":
		return "成功"
	case "failure", "startup_failure":
		return "失败"
	case "cancelled":
		return "已取消"
	case "timed_out":
		return "超时"
	case "action_required":
		return "需处理"
	case "skipped":
		return "已跳过"
	case "in_progress", "queued", "pending":
		return "进行中"
	default:
		if IsFailureConclusion(conclusion) {
			return "失败"
		}
		return "已完成"
	}
}

// FailedConclusions 返回失败结论列表，供 SQL IN 查询使用；每次返回新切片，外部修改不影响内部集合。
func FailedConclusions() []string {
	out := make([]string, 0, len(failedConclusionSet))
	for k := range failedConclusionSet {
		out = append(out, k)
	}
	return out
}
