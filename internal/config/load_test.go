package config

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoad无文件无环境变量返回安全默认值(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}

	if cfg.HTTP.Addr != "127.0.0.1:8080" {
		t.Fatalf("HTTP 地址=%q", cfg.HTTP.Addr)
	}
	if cfg.Database.Driver != "sqlite" || cfg.Database.URL != "file:/data/reposentinel.db" {
		t.Fatalf("数据库默认值=(%q,%q)", cfg.Database.Driver, cfg.Database.URL)
	}
	if cfg.Database.MaxOpenConns != 1 || cfg.Database.MaxIdleConns != 1 {
		t.Fatalf("SQLite 连接数=(%d,%d)", cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	}
	if cfg.Admin.SessionTTL != 24*time.Hour {
		t.Fatalf("Session TTL=%s", cfg.Admin.SessionTTL)
	}
}

func Test配置文件覆盖默认值(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("http:\n  addr: 127.0.0.1:8181\ndatabase:\n  url: file:/tmp/reposentinel.db\nlogging:\n  format: text\n  level: warn\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载 YAML 配置失败: %v", err)
	}

	if cfg.HTTP.Addr != "127.0.0.1:8181" {
		t.Fatalf("HTTP 地址=%q", cfg.HTTP.Addr)
	}
	if cfg.Database.URL != "file:/tmp/reposentinel.db" {
		t.Fatalf("SQLite URL=%q", cfg.Database.URL)
	}
	if cfg.Logging.Format != "text" || cfg.Logging.Level != "warn" {
		t.Fatalf("日志配置=(%q,%q)", cfg.Logging.Format, cfg.Logging.Level)
	}
}

func Test环境变量优先于配置文件(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("http:\n  addr: 127.0.0.1:8080\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv: func(name string) (string, bool) {
			if name == "REPOSENTINEL_HTTP_ADDR" {
				return "127.0.0.1:9090", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" {
		t.Fatalf("HTTP 地址=%q", cfg.HTTP.Addr)
	}
}

func Test标准日志环境变量优先于前缀别名和配置文件(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("logging:\n  format: json\n  level: warn\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv: lookupFromMap(map[string]string{
			"LOG_FORMAT":              "text",
			"LOG_LEVEL":               "debug",
			"REPOSENTINEL_LOG_FORMAT": "json",
			"REPOSENTINEL_LOG_LEVEL":  "error",
		}),
	})
	if err != nil {
		t.Fatalf("加载日志环境变量失败: %v", err)
	}
	if cfg.Logging.Format != "text" || cfg.Logging.Level != "debug" {
		t.Fatalf("日志配置=(%q,%q)，期望标准 LOG_* 变量优先", cfg.Logging.Format, cfg.Logging.Level)
	}
}

func Test未知YAML字段被拒绝(t *testing.T) {
	const secretSource = "未知字段错误不得泄漏-5e2a"
	_, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("admin:\n  username: admin\n  password: " + secretSource + "\nhttp:\n  addr: 127.0.0.1:8080\n  unexpected: true\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if err == nil {
		t.Fatal("未知 YAML 字段未被拒绝")
	}
	if strings.Contains(err.Error(), secretSource) {
		t.Fatal("未知字段错误泄漏了 Secret 明文")
	}
}

func Test全部批准环境变量映射到配置(t *testing.T) {
	currentKey := strings.Repeat("ab", 32)
	previousKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 32)))
	env := map[string]string{
		"REPOSENTINEL_HTTP_ADDR":                      "127.0.0.1:9191",
		"REPOSENTINEL_PUBLIC_BASE_URL":                "https://reposentinel.example.com",
		"REPOSENTINEL_DATABASE_DRIVER":                "postgres",
		"REPOSENTINEL_DATABASE_URL":                   "postgres://env-user:db-password-no-output@db.example.com/reposentinel",
		"REPOSENTINEL_DATABASE_MAX_OPEN_CONNS":        "27",
		"REPOSENTINEL_DATABASE_MAX_IDLE_CONNS":        "13",
		"REPOSENTINEL_ADMIN_USERNAME":                 "operator",
		"REPOSENTINEL_ADMIN_PASSWORD":                 "admin-env-secret-a821",
		"REPOSENTINEL_ADMIN_SESSION_TTL":              "36h",
		"REPOSENTINEL_SETUP_ALLOW_REMOTE":             "true",
		"REPOSENTINEL_ENCRYPTION_KEY":                 currentKey,
		"REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS":        previousKey,
		"REPOSENTINEL_GITHUB_APP_ID":                  "987654",
		"REPOSENTINEL_GITHUB_CLIENT_ID":               "github-client-id",
		"REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH":        "/run/secrets/github-app.pem",
		"REPOSENTINEL_GITHUB_WEBHOOK_SECRET":          "github-webhook-secret-c731",
		"REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET": "github-webhook-previous-d942",
		"REPOSENTINEL_EXTERNAL_PAT":                   "external-pat-e153",
		"REPOSENTINEL_TELEGRAM_TOKEN":                 "telegram-token-f264",
		"REPOSENTINEL_TELEGRAM_CHAT_ID":               "-1001234567890",
		"REPOSENTINEL_HTTP_WEBHOOK_URL":               "https://hooks.example.com/reposentinel",
		"REPOSENTINEL_HTTP_WEBHOOK_SECRET":            "http-webhook-secret-g375",
		"REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE":     "true",
		"REPOSENTINEL_LOG_FORMAT":                     "text",
		"REPOSENTINEL_LOG_LEVEL":                      "debug",
	}

	cfg, err := Load(context.Background(), LoadOptions{LookupEnv: lookupFromMap(env)})
	if err != nil {
		t.Fatalf("加载环境变量失败: %v", err)
	}

	if cfg.HTTP.Addr != env["REPOSENTINEL_HTTP_ADDR"] || cfg.HTTP.PublicBaseURL != env["REPOSENTINEL_PUBLIC_BASE_URL"] {
		t.Fatal("HTTP 环境变量未完整映射")
	}
	if cfg.Database.Driver != "postgres" || cfg.Database.URL != env["REPOSENTINEL_DATABASE_URL"] {
		t.Fatal("数据库环境变量未完整映射")
	}
	if cfg.Database.MaxOpenConns != 27 || cfg.Database.MaxIdleConns != 13 {
		t.Fatalf("数据库连接数=(%d,%d)", cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	}
	if cfg.Admin.Username != "operator" || cfg.Admin.Password.Reveal() != env["REPOSENTINEL_ADMIN_PASSWORD"] || cfg.Admin.SessionTTL != 36*time.Hour {
		t.Fatal("管理员环境变量未完整映射")
	}
	if !cfg.Setup.AllowRemote {
		t.Fatal("远程 Setup 环境变量未映射")
	}
	if cfg.Encryption.CurrentKey.Reveal() != currentKey || cfg.Encryption.PreviousKey.Reveal() != previousKey {
		t.Fatal("加密密钥环境变量未完整映射")
	}
	if cfg.GitHub.AppID != 987654 || cfg.GitHub.ClientID != "github-client-id" || cfg.GitHub.PrivateKeyPath != "/run/secrets/github-app.pem" {
		t.Fatal("GitHub 非 Secret 环境变量未完整映射")
	}
	if cfg.GitHub.WebhookSecret.Reveal() != env["REPOSENTINEL_GITHUB_WEBHOOK_SECRET"] ||
		cfg.GitHub.WebhookPreviousSecret.Reveal() != env["REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET"] ||
		cfg.GitHub.ExternalPAT.Reveal() != env["REPOSENTINEL_EXTERNAL_PAT"] {
		t.Fatal("GitHub Secret 环境变量未完整映射")
	}
	if cfg.Notify.Telegram.Token.Reveal() != env["REPOSENTINEL_TELEGRAM_TOKEN"] || cfg.Notify.Telegram.ChatID != "-1001234567890" {
		t.Fatal("Telegram 环境变量未完整映射")
	}
	if cfg.Notify.HTTPWebhook.URL != "https://hooks.example.com/reposentinel" ||
		cfg.Notify.HTTPWebhook.Secret.Reveal() != env["REPOSENTINEL_HTTP_WEBHOOK_SECRET"] ||
		!cfg.Notify.HTTPWebhook.AllowPrivate {
		t.Fatal("HTTP Webhook 环境变量未完整映射")
	}
	if cfg.Logging.Format != "text" || cfg.Logging.Level != "debug" {
		t.Fatal("日志环境变量未完整映射")
	}
}

func TestPostgres未显式设置最大连接数时使用十(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		wantMax int
	}{
		{
			name:    "YAML 只切换驱动",
			yaml:    "database:\n  driver: postgres\n  url: postgres://db.example.com/reposentinel\n",
			wantMax: 10,
		},
		{
			name: "环境变量只切换驱动",
			env: map[string]string{
				"REPOSENTINEL_DATABASE_DRIVER": "postgres",
				"REPOSENTINEL_DATABASE_URL":    "postgres://db.example.com/reposentinel",
			},
			wantMax: 10,
		},
		{
			name:    "显式值保持不变",
			yaml:    "database:\n  driver: postgres\n  url: postgres://db.example.com/reposentinel\n  max_open_conns: 23\n",
			wantMax: 23,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := LoadOptions{LookupEnv: lookupFromMap(tt.env)}
			if tt.yaml != "" {
				options.ConfigPath = "config.yaml"
				options.FileSystem = fstest.MapFS{"config.yaml": {Data: []byte(tt.yaml)}}
			}

			cfg, err := Load(context.Background(), options)
			if err != nil {
				t.Fatalf("加载 PostgreSQL 配置失败: %v", err)
			}
			if cfg.Database.MaxOpenConns != tt.wantMax {
				t.Fatalf("MaxOpenConns=%d，期望 %d", cfg.Database.MaxOpenConns, tt.wantMax)
			}
		})
	}
}

func TestPostgres连接池默认值与显式配置优先级(t *testing.T) {
	// 保护行为：postgres 驱动且连接池参数未显式配置时，MaxOpenConns=10 且
	// MaxIdleConns 自动对齐 MaxOpenConns（空闲连接与上限同量级，避免每请求重建 TCP 连接）；
	// 任一参数经 YAML 或环境变量显式设置后以显式值为准，默认推导不得覆盖用户意图。
	tests := []struct {
		name     string
		yaml     string
		env      map[string]string
		wantOpen int
		wantIdle int
	}{
		{
			name:     "未显式配置池参数时 idle 自动对齐 open",
			yaml:     "database:\n  driver: postgres\n  url: postgres://db.example.com/reposentinel\n",
			wantOpen: 10,
			wantIdle: 10,
		},
		{
			name: "仅环境变量显式 max_idle_conns 时 open 仍为默认十",
			env: map[string]string{
				"REPOSENTINEL_DATABASE_DRIVER":         "postgres",
				"REPOSENTINEL_DATABASE_URL":            "postgres://db.example.com/reposentinel",
				"REPOSENTINEL_DATABASE_MAX_IDLE_CONNS": "3",
			},
			wantOpen: 10,
			wantIdle: 3,
		},
		{
			name:     "YAML 显式两者时默认推导不得覆盖",
			yaml:     "database:\n  driver: postgres\n  url: postgres://db.example.com/reposentinel\n  max_open_conns: 25\n  max_idle_conns: 7\n",
			wantOpen: 25,
			wantIdle: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := LoadOptions{LookupEnv: lookupFromMap(tt.env)}
			if tt.yaml != "" {
				options.ConfigPath = "config.yaml"
				options.FileSystem = fstest.MapFS{"config.yaml": {Data: []byte(tt.yaml)}}
			}

			cfg, err := Load(context.Background(), options)
			if err != nil {
				t.Fatalf("加载 PostgreSQL 配置失败: %v", err)
			}
			if cfg.Database.MaxOpenConns != tt.wantOpen || cfg.Database.MaxIdleConns != tt.wantIdle {
				t.Fatalf("连接池=(%d,%d)，期望 (%d,%d)",
					cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, tt.wantOpen, tt.wantIdle)
			}
		})
	}
}

func Test配置解析错误安全且标明变量或字段(t *testing.T) {
	tests := []struct {
		name     string
		variable string
		field    string
		value    string
		yaml     string
	}{
		{name: "最大打开连接数", variable: "REPOSENTINEL_DATABASE_MAX_OPEN_CONNS", value: "invalid-open-612"},
		{name: "最大空闲连接数", variable: "REPOSENTINEL_DATABASE_MAX_IDLE_CONNS", value: "invalid-idle-723"},
		{name: "GitHub App ID", variable: "REPOSENTINEL_GITHUB_APP_ID", value: "invalid-app-834"},
		{name: "管理员会话时长", variable: "REPOSENTINEL_ADMIN_SESSION_TTL", value: "invalid-duration-945"},
		{name: "远程 Setup 布尔值", variable: "REPOSENTINEL_SETUP_ALLOW_REMOTE", value: "invalid-bool-a56"},
		{name: "Webhook 私网布尔值", variable: "REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE", value: "invalid-bool-b67"},
		{name: "YAML 整数", field: "database.max_open_conns", value: "invalid-yaml-int-c78", yaml: "database:\n  max_open_conns: invalid-yaml-int-c78\n"},
		{name: "YAML 布尔值", field: "setup.allow_remote", value: "invalid-yaml-bool-d89", yaml: "setup:\n  allow_remote: invalid-yaml-bool-d89\n"},
		{name: "YAML 时长", field: "admin.session_ttl", value: "invalid-yaml-duration-e90", yaml: "admin:\n  session_ttl: invalid-yaml-duration-e90\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := LoadOptions{LookupEnv: lookupFromMap(map[string]string{tt.variable: tt.value})}
			wantLocation := tt.variable
			if tt.yaml != "" {
				options.ConfigPath = "config.yaml"
				options.FileSystem = fstest.MapFS{"config.yaml": {Data: []byte(tt.yaml)}}
				wantLocation = tt.field
			}
			_, err := Load(context.Background(), options)
			validationErr := requireValidationError(t, err)
			if !strings.Contains(validationErr.Error(), wantLocation) {
				t.Fatalf("错误未标明变量或字段 %s: %v", wantLocation, validationErr)
			}
			if strings.Contains(validationErr.Error(), tt.value) {
				t.Fatal("解析错误回显了失败输入")
			}
		})
	}
}

func TestYAML与OS适配器受控加载Secret(t *testing.T) {
	const yamlSecret = "yaml-secret-c89"
	cfg, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("admin:\n  username: admin\n  password: " + yamlSecret + "\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载 YAML Secret 失败: %v", err)
	}
	if cfg.Admin.Password.Reveal() != yamlSecret {
		t.Fatal("YAML Secret 未通过受控解码保存")
	}

	t.Run("nil LookupEnv 使用操作系统环境", func(t *testing.T) {
		t.Setenv("REPOSENTINEL_HTTP_ADDR", "127.0.0.1:9292")
		loaded, err := Load(context.Background(), LoadOptions{})
		if err != nil {
			t.Fatalf("读取操作系统环境失败: %v", err)
		}
		if loaded.HTTP.Addr != "127.0.0.1:9292" {
			t.Fatalf("HTTP 地址=%q", loaded.HTTP.Addr)
		}
	})

	t.Run("nil FileSystem 使用操作系统文件", func(t *testing.T) {
		_, currentFile, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("无法定位测试文件")
		}
		examplePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "configs", "reposentinel.example.yaml")
		if _, err := Load(context.Background(), LoadOptions{
			ConfigPath: examplePath,
			LookupEnv:  func(string) (string, bool) { return "", false },
		}); err != nil {
			t.Fatalf("读取操作系统配置文件失败: %v", err)
		}
	})
}

func Test加载验证错误不泄漏敏感配置(t *testing.T) {
	const (
		databaseURL = "postgres://sensitive-user:database-password-d78@db.example.com/reposentinel"
		adminSecret = "admin-password-e89"
	)
	_, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("http:\n  addr: invalid-address\ndatabase:\n  driver: postgres\n  url: " + databaseURL + "\nadmin:\n  username: admin\n  password: " + adminSecret + "\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if err == nil {
		t.Fatal("无效 HTTP 地址未被拒绝")
	}
	for _, sensitive := range []string{databaseURL, "database-password-d78", adminSecret} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatal("加载错误泄漏了敏感配置")
		}
	}
}

func Test安全默认值(t *testing.T) {
	cfg := defaultConfig()

	if cfg.HTTP.Addr != "127.0.0.1:8080" {
		t.Fatalf("HTTP 地址=%q，期望安全默认地址", cfg.HTTP.Addr)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("数据库驱动=%q，期望 sqlite", cfg.Database.Driver)
	}
	if cfg.Database.URL != "file:/data/reposentinel.db" {
		t.Fatalf("SQLite URL=%q", cfg.Database.URL)
	}
	if cfg.Database.MaxOpenConns != 1 || cfg.Database.MaxIdleConns != 1 {
		t.Fatalf("SQLite 连接数=(%d,%d)，期望 (1,1)", cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	}
	if cfg.Logging.Format != "json" || cfg.Logging.Level != "info" {
		t.Fatalf("日志默认值=(%q,%q)，期望 (json,info)", cfg.Logging.Format, cfg.Logging.Level)
	}
	if cfg.Setup.AllowRemote {
		t.Fatal("Setup 默认不应允许远程访问")
	}
	if cfg.Admin.SessionTTL != 24*time.Hour {
		t.Fatalf("Session TTL=%s，期望 24h", cfg.Admin.SessionTTL)
	}
}

func Test阶段一配置类型包含批准字段(t *testing.T) {
	secret := NewSecret("类型检查用秘密")
	cfg := Config{
		HTTP: HTTPConfig{
			Addr:          "127.0.0.1:8080",
			PublicBaseURL: "https://reposentinel.example.com",
		},
		Database: DatabaseConfig{
			Driver:       "postgres",
			URL:          "postgres://reposentinel.example.com/reposentinel",
			MaxOpenConns: 10,
			MaxIdleConns: 10,
		},
		Admin: AdminBootstrapConfig{
			Username:   "admin",
			Password:   secret,
			SessionTTL: time.Hour,
		},
		Setup: SetupConfig{AllowRemote: true},
		Encryption: EncryptionConfig{
			CurrentKey:  secret,
			PreviousKey: secret,
		},
		GitHub: GitHubConfig{
			AppID:                 1,
			ClientID:              "client-id",
			PrivateKeyPath:        "/run/secrets/github.pem",
			WebhookSecret:         secret,
			WebhookPreviousSecret: secret,
			ExternalPAT:           secret,
		},
		Notify: NotifyConfig{
			Telegram: TelegramConfig{Token: secret, ChatID: "chat-id"},
			HTTPWebhook: HTTPWebhookConfig{
				URL:          "https://hooks.example.com/reposentinel",
				Secret:       secret,
				AllowPrivate: true,
			},
		},
		Logging: LoggingConfig{Format: "text", Level: "debug"},
	}
	options := LoadOptions{
		ConfigPath: "config.yaml",
		FileSystem: fstest.MapFS{},
		LookupEnv:  func(string) (string, bool) { return "", false },
	}

	if cfg.GitHub.AppID != 1 || cfg.Notify.Telegram.ChatID != "chat-id" {
		t.Fatal("完整配置字段未按输入保存")
	}
	var fileSystem fs.FS = options.FileSystem
	if fileSystem == nil || options.ConfigPath == "" || options.LookupEnv == nil {
		t.Fatal("LoadOptions 字段未按输入保存")
	}
}

func TestSecret仅Reveal可见明文(t *testing.T) {
	const source = "绝不能泄漏-secret-7f9c"
	secret := NewSecret(source)

	if got := secret.Reveal(); got != source {
		t.Fatalf("Reveal()=%q", got)
	}

	text, err := secret.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() 失败: %v", err)
	}
	jsonValue, err := secret.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() 失败: %v", err)
	}

	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	logger.Info("secret-check", "secret", secret)

	outputs := map[string]string{
		"String":            secret.String(),
		"GoString":          secret.GoString(),
		"fmt %s":            fmt.Sprintf("%s", secret),
		"fmt %q":            fmt.Sprintf("%q", secret),
		"fmt %v":            fmt.Sprintf("%v", secret),
		"fmt %+v":           fmt.Sprintf("%+v", secret),
		"fmt %#v":           fmt.Sprintf("%#v", secret),
		"MarshalText":       string(text),
		"MarshalJSON":       string(jsonValue),
		"slog.LogValue":     secret.LogValue().String(),
		"slog JSON handler": logOutput.String(),
	}
	for name, output := range outputs {
		if strings.Contains(output, source) {
			t.Errorf("%s 泄漏了 Secret 明文: %q", name, output)
		}
		if output == "" {
			t.Errorf("%s 未返回可识别的掩码", name)
		}
	}

	var decoded string
	if err := json.Unmarshal(jsonValue, &decoded); err != nil {
		t.Fatalf("MarshalJSON() 未返回有效 JSON 字符串: %v", err)
	}
	if decoded == "" || decoded == source {
		t.Fatalf("JSON 掩码=%q", decoded)
	}
}

func TestSecret使用私有字段阻止底层字符串转换(t *testing.T) {
	secretType := reflect.TypeOf(Secret{})
	if secretType.Kind() != reflect.Struct {
		t.Fatalf("Secret 底层类型=%s，期望私有字段结构体", secretType.Kind())
	}
	if secretType.NumField() != 1 || secretType.Field(0).PkgPath == "" {
		t.Fatal("Secret 明文字段必须保持私有")
	}
}

func lookupFromMap(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestAI环境变量解析与默认值(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		LookupEnv: lookupFromMap(map[string]string{
			"REPOSENTINEL_AI_ENABLED":        "true",
			"REPOSENTINEL_AI_BASE_URL":       "http://127.0.0.1:11434/v1",
			"REPOSENTINEL_AI_API_KEY":        "sk-test-key",
			"REPOSENTINEL_AI_MODEL":          "llama3.1",
			"REPOSENTINEL_AI_TIMEOUT":        "45s",
			"REPOSENTINEL_AI_MAX_TOKENS":     "1024",
			"REPOSENTINEL_AI_DIGEST_ENABLED": "false",
		}),
	})
	if err != nil {
		t.Fatalf("加载 AI 配置失败: %v", err)
	}
	if !cfg.AI.Enabled {
		t.Fatal("期望 ai.enabled=true")
	}
	if cfg.AI.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("BaseURL=%q", cfg.AI.BaseURL)
	}
	if cfg.AI.APIKey.Reveal() != "sk-test-key" {
		t.Fatal("API Key 未从环境变量注入")
	}
	if cfg.AI.Model != "llama3.1" {
		t.Fatalf("Model=%q", cfg.AI.Model)
	}
	if cfg.AI.Timeout != 45*time.Second {
		t.Fatalf("Timeout=%s", cfg.AI.Timeout)
	}
	if cfg.AI.MaxTokens != 1024 {
		t.Fatalf("MaxTokens=%d", cfg.AI.MaxTokens)
	}
	if cfg.AI.DigestEnabled {
		t.Fatal("期望 digest_enabled=false")
	}
	if !cfg.AI.TriageEnabled {
		t.Fatal("期望 triage_enabled 默认 true")
	}
}

func TestAI默认关闭(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}
	if cfg.AI.Enabled {
		t.Fatal("AI 默认必须关闭")
	}
	if cfg.AI.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("默认 BaseURL=%q", cfg.AI.BaseURL)
	}
	if cfg.AI.Model != "gpt-4o-mini" {
		t.Fatalf("默认 Model=%q", cfg.AI.Model)
	}
	if cfg.AI.Timeout != 20*time.Second {
		t.Fatalf("默认 Timeout=%s", cfg.AI.Timeout)
	}
}

func TestAIYAML解析(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{
		FileSystem: fstest.MapFS{
			"config.yaml": {Data: []byte("ai:\n  enabled: true\n  model: local-model\n  max_tokens: 512\n")},
		},
		ConfigPath: "config.yaml",
		LookupEnv: func(name string) (string, bool) {
			if name == "REPOSENTINEL_AI_API_KEY" {
				return "sk-yaml", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("加载 YAML AI 配置失败: %v", err)
	}
	if !cfg.AI.Enabled || cfg.AI.Model != "local-model" || cfg.AI.MaxTokens != 512 {
		t.Fatalf("YAML AI 配置=(%v,%q,%d)", cfg.AI.Enabled, cfg.AI.Model, cfg.AI.MaxTokens)
	}
	if cfg.AI.APIKey.Reveal() != "sk-yaml" {
		t.Fatal("API Key 应从环境变量注入")
	}
}
