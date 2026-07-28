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
	}
}
