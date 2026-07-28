package config

import (
	"io/fs"
	"time"
)

// Config 汇总 RepoSentinel 的全部配置。
type Config struct {
	HTTP       HTTPConfig           `yaml:"http"`
	Database   DatabaseConfig       `yaml:"database"`
	Admin      AdminBootstrapConfig `yaml:"admin"`
	Setup      SetupConfig          `yaml:"setup"`
	Encryption EncryptionConfig     `yaml:"encryption"`
	GitHub     GitHubConfig         `yaml:"github"`
	Notify     NotifyConfig         `yaml:"notify"`
	Logging    LoggingConfig        `yaml:"logging"`
	Metrics    MetricsConfig        `yaml:"metrics"`
}

// MetricsConfig 描述 Prometheus /metrics 暴露策略。
type MetricsConfig struct {
	// Enabled 默认 true；设为 false 可关闭路由注册。
	Enabled bool `yaml:"enabled"`
	// Token 可选 Bearer；非空时 /metrics 必须携带 Authorization: Bearer <token>。
	Token Secret `yaml:"token"`
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
