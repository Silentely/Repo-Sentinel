package config

import (
	"io/fs"
	"time"
)

// Config 汇总 RepoSentinel 的全部配置。
type Config struct {
	HTTP        HTTPConfig           `yaml:"http"`
	Database    DatabaseConfig       `yaml:"database"`
	Admin       AdminBootstrapConfig `yaml:"admin"`
	Setup       SetupConfig          `yaml:"setup"`
	Encryption  EncryptionConfig     `yaml:"encryption"`
	GitHub      GitHubConfig         `yaml:"github"`
	Notify      NotifyConfig         `yaml:"notify"`
	Logging     LoggingConfig        `yaml:"logging"`
	Metrics     MetricsConfig        `yaml:"metrics"`
	UpdateCheck UpdateCheckConfig    `yaml:"update_check"`
	Aggregation AggregationConfig    `yaml:"aggregation"`
}

// MetricsConfig 描述 Prometheus /metrics 暴露策略。
type MetricsConfig struct {
	// Enabled 默认 true；设为 false 可关闭路由注册。
	Enabled bool `yaml:"enabled"`
	// Token 可选 Bearer；非空时 /metrics 必须携带 Authorization: Bearer <token>。
	Token Secret `yaml:"token"`
}

// UpdateCheckConfig 描述关于页远程版本检查（对齐 TG-SignPulse：可关、HTTPS、soft-fail）。
type UpdateCheckConfig struct {
	// Enabled 默认 true；内网可关。
	Enabled bool `yaml:"enabled"`
	// URL 默认 GitHub API releases/latest；可换自定义 HTTPS JSON 源。
	URL string `yaml:"url"`
	// Token 可选，仅 JSON/API 路径使用（如私有仓库或提高配额）。
	Token Secret `yaml:"token"`
}

// AggregationConfig 描述通知短时合并窗口。多实例时依赖 Outbox 幂等键去重，进程内合并仍为 best-effort。
type AggregationConfig struct {
	// Window 同仓同类事件合并窗口，默认 60s。
	Window time.Duration `yaml:"window"`
	// BurstThreshold 超频阈值，默认 15。
	BurstThreshold int `yaml:"burst_threshold"`
	// BurstWindow 超频统计窗口，默认 5m。
	BurstWindow time.Duration `yaml:"burst_window"`
}

// LoadOptions 提供可注入的配置来源，避免加载过程依赖隐藏的全局状态。
type LoadOptions struct {
	ConfigPath string
	FileSystem fs.FS
	LookupEnv  func(string) (string, bool)
}

// HTTPConfig 描述 HTTP 监听地址与对外访问地址。
type HTTPConfig struct {
	Addr          string `yaml:"addr"`
	PublicBaseURL string `yaml:"public_base_url"`
}

// DatabaseConfig 描述数据库驱动、连接地址和连接池上限。
type DatabaseConfig struct {
	Driver       string `yaml:"driver"`
	URL          string `yaml:"url"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// AdminBootstrapConfig 描述初始管理员凭据与会话时长。
type AdminBootstrapConfig struct {
	Username   string        `yaml:"username"`
	Password   Secret        `yaml:"password"`
	SessionTTL time.Duration `yaml:"session_ttl"`
}

// SetupConfig 描述首次设置流程的远程访问策略。
type SetupConfig struct {
	AllowRemote bool `yaml:"allow_remote"`
}

// EncryptionConfig 描述当前与上一把数据加密密钥。
type EncryptionConfig struct {
	CurrentKey  Secret `yaml:"current_key"`
	PreviousKey Secret `yaml:"previous_key"`
}

// GitHubConfig 描述 GitHub App、Webhook 与外部仓库访问凭据。
type GitHubConfig struct {
	AppID                 int64  `yaml:"app_id"`
	ClientID              string `yaml:"client_id"`
	PrivateKeyPath        string `yaml:"private_key_path"`
	WebhookSecret         Secret `yaml:"webhook_secret"`
	WebhookPreviousSecret Secret `yaml:"webhook_previous_secret"`
	ExternalPAT           Secret `yaml:"external_pat"`
}

// NotifyConfig 汇总 Telegram 与 HTTP Webhook 通知配置。
type NotifyConfig struct {
	Telegram    TelegramConfig    `yaml:"telegram"`
	HTTPWebhook HTTPWebhookConfig `yaml:"http_webhook"`
}

// TelegramConfig 描述 Telegram Bot 通知目标。
type TelegramConfig struct {
	Token  Secret `yaml:"token"`
	ChatID string `yaml:"chat_id"`
}

// HTTPWebhookConfig 描述 HTTP Webhook 通知目标与私网策略。
type HTTPWebhookConfig struct {
	URL          string `yaml:"url"`
	Secret       Secret `yaml:"secret"`
	AllowPrivate bool   `yaml:"allow_private"`
}

// LoggingConfig 描述结构化日志格式与级别。
type LoggingConfig struct {
	Format string `yaml:"format"`
	Level  string `yaml:"level"`
}
