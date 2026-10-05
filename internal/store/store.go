package store

import (
	"context"
	"encoding/json"
	"strconv"
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
	Leases() LeaseStore
	Diagnostics() DiagnosticStore
	Dashboard(context.Context) (DashboardStats, error)
	// PingQuick 快速探测底层数据库连通性（建议带短超时 1~2s），避免常规探测长时间挂起。
	PingQuick(context.Context) error
	// StarTrend 汇总活跃监控仓的 star 快照为按日总趋势；days<=0 表示全部。
	StarTrend(context.Context, int) ([]StarTrendPoint, error)
	// CleanupRetention 按策略删除过期事件、终态 Outbox 与旧 Webhook Delivery。
	CleanupRetention(context.Context, RetentionPolicy, time.Time) (CleanupResult, error)
	// CleanupTransientSettings 清理可再生的临时设置行（ChatOps 令牌/领取标记、自动打标回执），
	// 返回删除行数。这些行只为短期幂等或一次性消费存在，长期残留会让 settings 表无界增长。
	CleanupTransientSettings(context.Context, time.Time) (int, error)
	WithTx(context.Context, func(Store) error) error
	Close() error
}

// ChatOps 交互令牌的 settings 键。令牌由按钮回调一次性消费，
// 领取标记（claim）用于保证同一令牌只有一个消费者成功。
// store 的过期清理与 httpapi 的读写共用这两个构造函数，避免键格式分叉。
const (
	chatOpsTokenKeyPrefix = "chatops_token:"
	chatOpsClaimKeyPrefix = "chatops_claim:"
	// chatOpsClaimRetention 领取标记的保留时长：令牌 TTL 上限 15 分钟，
	// 取 1 小时留出执行与排障余量，避免误删仍在执行中的动作标记。
	chatOpsClaimRetention = time.Hour

	autoLabelReceiptKeyPrefix = "side_effect:auto_label:"
	// autoLabelReceiptRetention 自动打标回执的保留时长。回执只用于短期幂等：
	// 过期后如重新分诊会重复调用一次加标签接口（GitHub 侧幂等，无害），
	// 但不清理会让 settings 表随打标过的 Issue 数无界增长。
	autoLabelReceiptRetention = 90 * 24 * time.Hour
)

// ChatOpsTokenKey 返回交互令牌的 settings 键。
func ChatOpsTokenKey(id string) string { return chatOpsTokenKeyPrefix + id }

// ChatOpsClaimKey 返回令牌领取标记的 settings 键。
func ChatOpsClaimKey(id string) string { return chatOpsClaimKeyPrefix + id }

// AutoLabelReceiptKey 返回自动打标回执的 settings 键：同一 (仓库, Issue, 分类) 只打一次。
func AutoLabelReceiptKey(repoFullName string, issueNumber int, category string) string {
	return "side_effect:auto_label:" + repoFullName + ":" + strconv.Itoa(issueNumber) + ":v1:" + category
}

// legacyAutoLabelReceiptKeyPrefix 迁移前的自动打标回执键前缀。
const legacyAutoLabelReceiptKeyPrefix = "github_label:"

// LegacyAutoLabelReceiptKey 返回迁移前的自动打标回执键，用于对存量库做一次性兼容：
// 旧键命中说明升级前该 Issue 已打标，新逻辑据此跳过以免重复调用打标接口。
func LegacyAutoLabelReceiptKey(repoFullName string, issueNumber int, category string) string {
	return legacyAutoLabelReceiptKeyPrefix + repoFullName + ":" + strconv.Itoa(issueNumber) + ":" + category
}

const (
	muteSettingKeyPrefix = "mute:repo:"
	muteAllSettingKey    = "mute:all"
)

// MuteSettingKey 返回指定仓库的紧急静音设置键。
func MuteSettingKey(repoID string) string { return muteSettingKeyPrefix + repoID }

// MuteAllSettingKey 返回全局紧急静音设置键。
func MuteAllSettingKey() string { return muteAllSettingKey }

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
	// Create 仅创建新设置键；已存在时返回 ErrConflict，用于需要一次性竞争的状态迁移。
	Create(context.Context, SystemSetting) (SystemSetting, error)
	Delete(context.Context, string) error
	// GetMany 批量读取设置：仅返回存在的键，缺失的键不返回也不报错。
	GetMany(context.Context, ...string) ([]SystemSetting, error)
	Upsert(context.Context, SystemSetting) (SystemSetting, error)
	// UpdateAIBudgetUsageAtomic 原子累加当日 AI 预算用量，并返回是否触发软限流熔断。
	// 双轨方言安全实现，显式补齐 updated_by = 'ai_budget' 哨兵值。
	UpdateAIBudgetUsageAtomic(ctx context.Context, todayKey string, tokens int, costCents int, budgetLimitCents int) (isThrottled bool, err error)
}

// AuditStore 仅提供追加与只读查询，不暴露更新或删除。
type AuditStore interface {
	Append(context.Context, AuditLog) (AuditLog, error)
	List(context.Context, int, int) ([]AuditLog, error)
	ListCursor(context.Context, time.Time, string, int) ([]AuditLog, error)
	Get(context.Context, string) (AuditLog, error)
}
