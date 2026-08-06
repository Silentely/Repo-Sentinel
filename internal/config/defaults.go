package config

import "time"

const (
	defaultHTTPAddr             = "127.0.0.1:8080"
	defaultSQLiteURL            = "file:/data/reposentinel.db"
	defaultSQLiteMaxOpenConns   = 1
	defaultSQLiteMaxIdleConns   = 1
	defaultPostgresMaxOpenConns = 10
	defaultSessionTTL           = 24 * time.Hour
)

// defaultConfig 返回不依赖外部输入的安全基础配置。
func defaultConfig() Config {
	return Config{
		HTTP: HTTPConfig{
			Addr: defaultHTTPAddr,
		},
		Database: DatabaseConfig{
			Driver:       "sqlite",
			URL:          defaultSQLiteURL,
			MaxOpenConns: defaultSQLiteMaxOpenConns,
			MaxIdleConns: defaultSQLiteMaxIdleConns,
		},
		Admin: AdminBootstrapConfig{
			SessionTTL: defaultSessionTTL,
		},
		Setup: SetupConfig{
			AllowRemote: false,
		},
		Logging: LoggingConfig{
			Format: "json",
			Level:  "info",
		},
		Metrics: MetricsConfig{
			Enabled: true,
		},
		UpdateCheck: UpdateCheckConfig{
			Enabled: true,
			URL:     "https://api.github.com/repos/Silentely/Repo-Sentinel/releases/latest",
		},
		Aggregation: AggregationConfig{
			Window:         60 * time.Second,
			BurstThreshold: 15,
			BurstWindow:    5 * time.Minute,
		},
		// AI 标量字段（BaseURL/Model/Timeout/MaxTokens）不注入默认值：
		// 空值表示「未显式设置」，管理台可编辑；实际取值由 ai.Client 在使用点回退默认。
		// 仅 bool 开关保留默认，避免 RuntimeFromEnv 将其误判为 env 显式设置而锁定。
		AI: AIConfig{
			Enabled:       false,
			DigestEnabled: true,
			TriageEnabled: true,
		},
		// OAuth Agent 凭据无默认值：未配置时元数据照常发布，token 端点拒绝签发。
		OAuth: OAuthConfig{
			ClientID: "reposentinel-agent",
		},
	}
}
