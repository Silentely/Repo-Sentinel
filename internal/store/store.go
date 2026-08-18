package store

import (
	"context"
	"encoding/json"
	"time"
)

// AdminAccount 是存储层对外使用的管理员领域模型。
type AdminAccount struct {
	ID                string
	Username          string
	PasswordHash      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PasswordChangedAt time.Time
}

// AdminSession 是存储层对外使用的管理员会话领域模型。
type AdminSession struct {
	ID         string
	AdminID    string
	TokenHash  string
	CSRFHash   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	IPAddress  string
	UserAgent  string
}

// SystemSetting 是按唯一键保存的 JSON 系统设置。
type SystemSetting struct {
	ID        string
	Key       string
	ValueJSON json.RawMessage
	UpdatedAt time.Time
	UpdatedBy string
}

// AuditLog 是只能追加和读取的审计记录。
type AuditLog struct {
	ID           string
	Action       string
	ActorType    string
	ActorID      string
	TargetType   string
	TargetID     string
	MetadataJSON json.RawMessage
	IPAddress    string
	CreatedAt    time.Time
}

// Store 汇总持久化能力。
type Store interface {
	Admins() AdminStore
	Sessions() SessionStore
	Settings() SettingsStore
	Audits() AuditStore
	Installations() InstallationStore
	Repositories() RepositoryStore
	WebhookDeliveries() WebhookDeliveryStore
	WorkItems() WorkItemStore
	WorkflowRuns() WorkflowRunStore
	SecurityAlerts() SecurityAlertStore
	Events() EventStore
	RepoStatSnapshots() RepoStatSnapshotStore
	StarredTrackers() StarredTrackerStore
	Channels() ChannelStore
	Outbox() OutboxStore
	Cursors() CursorStore
	Dashboard(context.Context) (DashboardStats, error)
	// StarTrend 汇总活跃监控仓的 star 快照为按日总趋势；days<=0 表示全部。
	StarTrend(context.Context, int) ([]StarTrendPoint, error)
	// CleanupRetention 按策略删除过期事件、终态 Outbox 与旧 Webhook Delivery。
	CleanupRetention(context.Context, RetentionPolicy, time.Time) (CleanupResult, error)
	WithTx(context.Context, func(Store) error) error
	Close() error
}

// AdminStore 管理唯一管理员账号。
type AdminStore interface {
	Create(context.Context, AdminAccount) (AdminAccount, error)
	Get(context.Context, string) (AdminAccount, error)
	GetOnly(context.Context) (AdminAccount, error)
	FindByUsername(context.Context, string) (AdminAccount, error)
	UpdatePassword(context.Context, string, string, time.Time) (AdminAccount, error)
	// UpdatePasswordIfCurrent 仅在当前密码哈希匹配时原子更新密码。
	UpdatePasswordIfCurrent(
		ctx context.Context,
		id string,
		expectedHash string,
		newHash string,
		changedAt time.Time,
	) (bool, error)
	DeleteForTest(context.Context, string) error
}

// SessionStore 管理管理员会话；撤销通过立即删除实现。
// DeleteOthers 在 keepSessionID 为空时删除该管理员的全部 Session。
type SessionStore interface {
	Create(context.Context, AdminSession) (AdminSession, error)
	GetActiveByTokenHash(context.Context, string, time.Time) (AdminSession, error)
	Revoke(context.Context, string) error
	DeleteOthers(context.Context, string, string) (int, error)
	Touch(context.Context, string, time.Time) (AdminSession, error)
	CleanupExpired(context.Context, time.Time) (int, error)
}

// SettingsStore 管理唯一键系统设置。
type SettingsStore interface {
	Get(context.Context, string) (SystemSetting, error)
	// GetMany 批量读取设置：仅返回存在的键，缺失的键不返回也不报错。
	GetMany(context.Context, ...string) ([]SystemSetting, error)
	Upsert(context.Context, SystemSetting) (SystemSetting, error)
}

// AuditStore 仅提供追加与只读查询，不暴露更新或删除。
type AuditStore interface {
	Append(context.Context, AuditLog) (AuditLog, error)
	List(context.Context, int, int) ([]AuditLog, error)
	Get(context.Context, string) (AuditLog, error)
}
