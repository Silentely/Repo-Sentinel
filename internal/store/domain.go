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

	AlertKindDependabot     = "dependabot"
	AlertKindCodeScanning   = "code_scanning"
	AlertKindSecretScanning = "secret_scanning"

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
	HTMLURL            string     `json:"html_url"`
	DefaultBranch      string     `json:"default_branch"`
	BaselineStartedAt  *time.Time `json:"baseline_started_at,omitempty"`
	BaselineFinishedAt *time.Time `json:"baseline_finished_at,omitempty"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	LastSyncErrorCode  string     `json:"last_sync_error_code,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
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
	Kind            string    `json:"kind"`
	State           string    `json:"state"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	LabelsJSON      []any     `json:"labels,omitempty"`
	AssigneesJSON   []any     `json:"assignees,omitempty"`
	Milestone       string    `json:"milestone,omitempty"`
	Draft           bool      `json:"draft"`
	Merged          bool      `json:"merged"`
	HTMLURL         string    `json:"html_url"`
	SourceUpdatedAt time.Time `json:"source_updated_at"`
	StateHash       string    `json:"state_hash,omitempty"`
	// 新增 Review 相关字段
	ReviewState    string   `json:"review_state,omitempty"`    // 最新审核状态：APPROVED, CHANGES_REQUESTED, COMMENTED, PENDING
	ReviewDecision string   `json:"review_decision,omitempty"` // 审核决策：approved, changes_requested
	Reviewers      []string `json:"reviewers,omitempty"`       // 已请求审核的人
	// 新增 Check Runs 相关字段
	CheckStatus     string `json:"check_status,omitempty"`     // 检查状态：success, failure, pending
	CheckConclusion string `json:"check_conclusion,omitempty"` // 检查结论
	ChecksTotal     int    `json:"checks_total,omitempty"`     // 总检查数
	ChecksPassed    int    `json:"checks_passed,omitempty"`    // 通过检查数
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// SecurityAlert 领域模型。
type SecurityAlert struct {
	ID               string    `json:"id"`
	RepositoryID     string    `json:"repository_id"`
	RepositoryFullName string  `json:"repository_full_name,omitempty"`
	AlertKind        string    `json:"alert_kind"`
	AlertNumber      int       `json:"alert_number"`
	State            string    `json:"state"`
	Severity         string    `json:"severity"`
	RuleOrDependency string    `json:"rule_or_dependency"`
	DismissedReason  string    `json:"dismissed_reason,omitempty"`
	HTMLURL          string    `json:"html_url"`
	SourceUpdatedAt  time.Time `json:"source_updated_at"`
	StateHash        string    `json:"state_hash,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Event 规范化业务事件。
type Event struct {
	ID                   string         `json:"id"`
	Source               string         `json:"source"`
	Kind                 string         `json:"kind"`
	Action               string         `json:"action"`
	RepositoryID         *string        `json:"repository_id,omitempty"`
	SubjectNumber        *int           `json:"subject_number,omitempty"`
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

// NotificationChannel 通知渠道。
type NotificationChannel struct {
	ID             string    `json:"id"`
	ChannelType    string    `json:"channel_type"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	Target         string    `json:"target"`
	SecretEnvelope string    `json:"-"`
	AllowPrivate   bool      `json:"allow_private"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

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

// ListFilter 通用列表筛选。
type ListFilter struct {
	Page    int
	PerPage int
	// 可选筛选
	RepositoryID string
	Kind         string
	State        string
	Status       string
	Query        string
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
	UpdateSyncStatus(context.Context, string, string) error
	UpdateSettings(context.Context, string, RepositorySettings) error
	CountByType(context.Context, string) (int, error)
}

// RepositorySettings 仓库能力开关与归档设置。
type RepositorySettings struct {
	MonitorEnabled *bool `json:"monitor_enabled,omitempty"`
	IssuesEnabled  *bool `json:"issues_enabled,omitempty"`
	PrEnabled      *bool `json:"pr_enabled,omitempty"`
	ActionsEnabled *bool `json:"actions_enabled,omitempty"`
	AlertsEnabled  *bool `json:"alerts_enabled,omitempty"`
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
}

// WorkItemStore Issue/PR。
type WorkItemStore interface {
	GetByRepoNumber(context.Context, string, int) (WorkItem, error)
	UpsertIfNewer(context.Context, WorkItem) (WorkItem, bool, error) // bool=updated
	List(context.Context, ListFilter) ([]WorkItem, PageResult, error)
	CountOpen(context.Context) (int, error)
}

// WorkflowRunStore Actions。
type WorkflowRunStore interface {
	GetByRepoRunID(context.Context, string, int64) (WorkflowRun, error)
	UpsertIfNewer(context.Context, WorkflowRun) (WorkflowRun, bool, error)
	LatestCompleted(context.Context, string, int64, string) (WorkflowRun, error)
	List(context.Context, ListFilter) ([]WorkflowRun, PageResult, error)
	CountFailed(context.Context) (int, error)
}

// SecurityAlertStore 安全告警。
type SecurityAlertStore interface {
	GetByIdentity(context.Context, string, string, int) (SecurityAlert, error)
	UpsertIfNewer(context.Context, SecurityAlert) (SecurityAlert, bool, error)
	List(context.Context, ListFilter) ([]SecurityAlert, PageResult, error)
	CountOpen(context.Context) (int, error)
}

// EventStore 业务事件。
type EventStore interface {
	Create(context.Context, Event) (Event, error)
	GetByFingerprint(context.Context, string) (Event, error)
	List(context.Context, ListFilter) ([]Event, PageResult, error)
	CountSince(context.Context, time.Time) (int, error)
}

// ChannelStore 通知渠道。
type ChannelStore interface {
	Upsert(context.Context, NotificationChannel) (NotificationChannel, error)
	Get(context.Context, string) (NotificationChannel, error)
	GetEnabledByType(context.Context, string) (NotificationChannel, error)
	List(context.Context) ([]NotificationChannel, error)
	DisableOthersOfType(context.Context, string, string) error
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

// MustJSON 将对象编码为 RawMessage（测试与设置辅助）。
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
