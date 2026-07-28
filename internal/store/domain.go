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

	AlertKindDependabot    = "dependabot"
	AlertKindCodeScanning  = "code_scanning"
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
	ID               string
	InstallationID   int64
	AccountLogin     string
	AccountType      string
	TargetType       string
	PermissionsJSON  map[string]any
	Suspended        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Repository 领域模型。
type Repository struct {
	ID                 string
	Type               string
	SyncStatus         string
	GitHubRepoID       *int64
	Owner              string
	Name               string
	FullName           string
	InstallationID     *string
	IsArchived         bool
	IsPrivate          bool
	HTMLURL            string
	DefaultBranch      string
	BaselineStartedAt  *time.Time
	BaselineFinishedAt *time.Time
	LastSyncedAt       *time.Time
	LastSyncErrorCode  string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// WebhookDelivery 领域模型。
type WebhookDelivery struct {
	ID                  string
	DeliveryID          string
	EventType           string
	Action              string
	RepositoryFullName  string
	Status              string
	ErrorCode           string
	Payload             []byte
	ReceivedAt          time.Time
	ProcessedAt         *time.Time
}

// WorkItem 领域模型。
type WorkItem struct {
	ID              string
	RepositoryID    string
	Number          int
	Kind            string
	State           string
	Title           string
	Author          string
	LabelsJSON      []any
	AssigneesJSON   []any
	Milestone       string
	Draft           bool
	Merged          bool
	HTMLURL         string
	SourceUpdatedAt time.Time
	StateHash       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// WorkflowRun 领域模型。
type WorkflowRun struct {
	ID                 string
	RepositoryID       string
	GitHubRunID        int64
	GitHubWorkflowID   int64
	WorkflowName       string
	RunNumber          int
	Event              string
	HeadBranch         string
	HeadSHA            string
	Status             string
	Conclusion         *string
	PreviousConclusion *string
	Actor              string
	RunAttempt         int
	HTMLURL            string
	RunStartedAt       *time.Time
	RunUpdatedAt       time.Time
	RunCompletedAt     *time.Time
	StateHash          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// SecurityAlert 领域模型。
type SecurityAlert struct {
	ID                string
	RepositoryID      string
	AlertKind         string
	AlertNumber       int
	State             string
	Severity          string
	RuleOrDependency  string
	DismissedReason   string
	HTMLURL           string
	SourceUpdatedAt   time.Time
	StateHash         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Event 规范化业务事件。
type Event struct {
	ID                   string
	Source               string
	Kind                 string
	Action               string
	RepositoryID         *string
	SubjectNumber        *int
	Title                string
	Severity             string
	Actor                string
	WorkflowRunID        *int64
	WorkflowConclusion   string
	OccurredAt           time.Time
	SourceUpdatedAt      *time.Time
	HTMLURL              string
	PayloadSummary       map[string]any
	SuppressNotification bool
	DedupeFingerprint    string
	StateHash            string
	CreatedAt            time.Time
}

// NotificationChannel 通知渠道。
type NotificationChannel struct {
	ID             string
	ChannelType    string
	Name           string
	Enabled        bool
	Target         string
	SecretEnvelope string
	AllowPrivate   bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NotificationOutbox 待投递通知。
type NotificationOutbox struct {
	ID             string
	ChannelID      string
	EventID        *string
	AggregateKey   string
	IdempotencyKey string
	Status         string
	AttemptCount   int
	NextAttemptAt  time.Time
	LockedUntil    *time.Time
	LastErrorCode  string
	Title          string
	BodyText       string
	BodyJSON       map[string]any
	ParseMode      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SyncCursor 同步游标。
type SyncCursor struct {
	ID             string
	RepositoryID   string
	Resource       string
	CursorValue    string
	ETag           string
	LastSuccessAt  *time.Time
	LastErrorCode  string
	UpdatedAt      time.Time
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
	Page    int
	PerPage int
	Total   int
}

// RepositoryStore 仓库持久化。
type RepositoryStore interface {
	Upsert(context.Context, Repository) (Repository, error)
	Get(context.Context, string) (Repository, error)
	GetByFullName(context.Context, string) (Repository, error)
	GetByGitHubRepoID(context.Context, int64) (Repository, error)
	List(context.Context, ListFilter) ([]Repository, PageResult, error)
	UpdateSyncStatus(context.Context, string, string) error
	CountByType(context.Context, string) (int, error)
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
	OpenIssues       int `json:"open_issues"`
	OpenPulls        int `json:"open_pulls"`
	FailedActions    int `json:"failed_actions"`
	OpenSecurity     int `json:"open_security"`
	Events24h        int `json:"events_24h"`
	OutboxDead       int `json:"outbox_dead"`
	ReposActive       int `json:"repos_active"`
	ReposBaseline     int `json:"repos_baseline"`
	ChannelsEnabled  int `json:"channels_enabled"`
}

// MustJSON 将对象编码为 RawMessage（测试与设置辅助）。
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
