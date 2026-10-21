package config

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func Test管理员用户名与密码必须成对(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password Secret
	}{
		{name: "只有用户名", username: "admin"},
		{name: "只有密码", password: NewSecret("仅用于配对测试")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Admin.Username = tt.username
			cfg.Admin.Password = tt.password

			err := cfg.Validate()
			if err == nil {
				t.Fatal("不完整的管理员凭据未被拒绝")
			}

			requireValidationError(t, err)
		})
	}
}

func Test数据库驱动与PostgresURL验证(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		url    string
	}{
		{name: "未知驱动", driver: "mysql", url: "ignored"},
		{name: "Postgres 缺少 URL", driver: "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Database.Driver = tt.driver
			cfg.Database.URL = tt.url
			requireValidationError(t, cfg.Validate())
		})
	}
}

func Test驱动与地址空白兼容(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.Driver = " sqlite "
	cfg.HTTP.Addr = " 127.0.0.1:8080 "
	if err := cfg.Validate(); err != nil {
		t.Fatalf("空白兼容失败: %v", err)
	}
}

func TestHTTP地址必须包含主机与端口(t *testing.T) {
	cfg := defaultConfig()
	cfg.HTTP.Addr = "127.0.0.1"
	requireValidationError(t, cfg.Validate())
}

func TestPublicBaseURL仅允许HTTPS或本机HTTP(t *testing.T) {
	valid := []string{
		"",
		"https://reposentinel.example.com",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
	}
	for _, value := range valid {
		t.Run("允许_"+value, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.HTTP.PublicBaseURL = value
			if err := cfg.Validate(); err != nil {
				t.Fatalf("合法 PublicBaseURL 被拒绝: %v", err)
			}
		})
	}

	invalid := []string{
		"http://reposentinel.example.com",
		"ftp://localhost/config",
		"https://",
	}
	for _, value := range invalid {
		t.Run("拒绝_"+value, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.HTTP.PublicBaseURL = value
			requireValidationError(t, cfg.Validate())
		})
	}
}

func Test日志级别与格式验证(t *testing.T) {
	tests := []struct {
		name   string
		format string
		level  string
	}{
		{name: "无效格式", format: "console", level: "info"},
		{name: "无效级别", format: "json", level: "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Logging.Format = tt.format
			cfg.Logging.Level = tt.level
			requireValidationError(t, cfg.Validate())
		})
	}
}

func Test主密钥接受32字节Base64或Hex(t *testing.T) {
	validBase64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	validHex := strings.Repeat("ab", 32)

	cfg := defaultConfig()
	cfg.Encryption.CurrentKey = NewSecret(validBase64)
	cfg.Encryption.PreviousKey = NewSecret(validHex)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("合法主密钥被拒绝: %v", err)
	}

	tests := []struct {
		name     string
		current  string
		previous string
	}{
		{name: "当前密钥编码无效", current: "invalid-key-format-c12"},
		{name: "当前密钥长度错误", current: strings.Repeat("ab", 31)},
		{name: "上一把密钥编码无效", current: validHex, previous: "invalid-previous-d23"},
		{name: "上一把密钥长度错误", current: validHex, previous: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 31)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Encryption.CurrentKey = NewSecret(tt.current)
			cfg.Encryption.PreviousKey = NewSecret(tt.previous)
			err := cfg.Validate()
			requireValidationError(t, err)
			if strings.Contains(err.Error(), tt.current) || (tt.previous != "" && strings.Contains(err.Error(), tt.previous)) {
				t.Fatal("密钥验证错误泄漏了 Secret 明文")
			}
		})
	}
}

func Test完整有效配置通过验证(t *testing.T) {
	cfg := defaultConfig()
	cfg.HTTP.PublicBaseURL = "https://reposentinel.example.com"
	cfg.Database = DatabaseConfig{
		Driver:       "postgres",
		URL:          "postgres://db.example.com/reposentinel",
		MaxOpenConns: 10,
		MaxIdleConns: 1,
	}
	cfg.Admin.Username = "admin"
	cfg.Admin.Password = NewSecret("valid-admin-secret-e34")
	cfg.Logging = LoggingConfig{Format: "text", Level: "error"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("完整有效配置未通过验证: %v", err)
	}
}

func TestValidationError稳定且不泄漏敏感配置(t *testing.T) {
	const (
		databaseURL = "postgres://secret-user:database-password-f45@db.example.com/reposentinel"
		adminSecret = "admin-password-g56"
	)
	cfg := defaultConfig()
	cfg.HTTP.Addr = "invalid-address"
	cfg.Database.Driver = "postgres"
	cfg.Database.URL = databaseURL
	cfg.Admin.Username = "admin"
	cfg.Admin.Password = NewSecret(adminSecret)

	err := cfg.Validate()
	validationErr := requireValidationError(t, err)
	if !strings.Contains(validationErr.Error(), "validation_failed") {
		t.Fatalf("错误文本缺少稳定错误码: %v", validationErr)
	}
	for _, sensitive := range []string{databaseURL, "database-password-f45", adminSecret} {
		if strings.Contains(validationErr.Error(), sensitive) {
			t.Fatal("ValidationError 泄漏了敏感配置")
		}
	}
}

func TestAI校验规则(t *testing.T) {
	t.Run("开启但缺 Key 拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("")
		if err := cfg.Validate(); err == nil {
			t.Fatal("ai.enabled=true 且无 API Key 应校验失败")
		}
	})
	t.Run("关闭时缺 Key 允许", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = false
		if err := cfg.Validate(); err != nil {
			t.Fatalf("AI 关闭时不应校验失败: %v", err)
		}
	})
	t.Run("非法超时拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("sk-test")
		cfg.AI.Timeout = -time.Second
		if err := cfg.Validate(); err == nil {
			t.Fatal("负超时应校验失败")
		}
	})
	t.Run("非法 max_tokens 拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("sk-test")
		cfg.AI.MaxTokens = -1
		if err := cfg.Validate(); err == nil {
			t.Fatal("负 max_tokens 应校验失败")
		}
	})
	t.Run("max_tokens 为 0 允许（使用默认 800）", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("sk-test")
		cfg.AI.MaxTokens = 0
		if err := cfg.Validate(); err != nil {
			t.Fatalf("max_tokens=0 应允许（默认值）：%v", err)
		}
	})
	t.Run("非法 base_url 拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("sk-test")
		cfg.AI.BaseURL = "ftp://invalid"
		if err := cfg.Validate(); err == nil {
			t.Fatal("非 http(s) base_url 应校验失败")
		}
	})
	t.Run("base_url 含 userinfo 拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.AI.Enabled = true
		cfg.AI.APIKey = NewSecret("sk-test")
		cfg.AI.BaseURL = "https://user:pass@api.example.com/v1"
		if err := cfg.Validate(); err == nil {
			t.Fatal("URL 内嵌凭据的 base_url 应校验失败（与 httpapi 校验一致）")
		}
	})
}

func requireValidationError(t *testing.T, err error) *ValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("期望 validation_failed，实际无错误")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("错误类型=%T，期望 ValidationError", err)
	}
	if validationErr.ErrorCode() != "validation_failed" {
		t.Fatalf("错误码=%q", validationErr.ErrorCode())
	}
	return validationErr
}

// SQLite 强制单连接：显式配置更大的连接池必须在启动前被拒绝，而非静默忽略。
func TestSqlite连接池配置被拒绝(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.Driver = "sqlite"
	cfg.Database.MaxOpenConns = 10
	if err := cfg.Validate(); err == nil {
		t.Fatal("sqlite + max_open_conns>1 应被拒绝")
	} else if !strings.Contains(err.Error(), "max_open_conns") {
		t.Fatalf("错误应指向 max_open_conns: %v", err)
	}
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 2
	if err := cfg.Validate(); err == nil {
		t.Fatal("sqlite + max_idle_conns>1 应被拒绝")
	} else if !strings.Contains(err.Error(), "max_idle_conns") {
		t.Fatalf("错误应指向 max_idle_conns: %v", err)
	}
	// 0（未设置）与 1 应通过。
	cfg.Database.MaxIdleConns = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("sqlite 默认连接池配置应通过: %v", err)
	}
}

func TestTrustedProxiesValidation(t *testing.T) {
	t.Run("有效IP与CIDR", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.HTTP.TrustedProxies = []string{
			"127.0.0.1",
			"::1",
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.1.100",
			"2001:db8::/32",
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("有效 trusted_proxies 被拒绝: %v", err)
		}
	})

	t.Run("空字符串元素被拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.HTTP.TrustedProxies = []string{"127.0.0.1", "  "}
		requireValidationError(t, cfg.Validate())
	})

	t.Run("非法IP或CIDR被拒绝", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.HTTP.TrustedProxies = []string{"invalid-ip", "10.0.0.0/8"}
		requireValidationError(t, cfg.Validate())
	})
}

func TestJevValidation(t *testing.T) {
	cfg := defaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("APIKey 必填", func(t *testing.T) {
		c := defaultConfig()
		c.Jev.Enabled = true
		c.Jev.APIKey = NewSecret("")
		var validation *ValidationError
		if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev.api_key" {
			t.Fatalf("错误字段不符: %v", err)
		}
	})

	t.Run("BaseURL 校验", func(t *testing.T) {
		invalidURLs := []string{
			"http://api.typesafe.ai",
			"https://",
			"https://user:pass@api.typesafe.ai",
			"https://api.typesafe.ai/v1",
			"https://api.typesafe.ai/custom/path",
			"https://api.typesafe.ai?query=1",
			"https://api.typesafe.ai#frag",
		}
		for _, u := range invalidURLs {
			c := defaultConfig()
			c.Jev.BaseURL = u
			var validation *ValidationError
			if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev.base_url" {
				t.Fatalf("url %q 必须被拒绝: %v", u, err)
			}
		}

		validURLs := []string{
			"https://api.typesafe.ai",
			"https://api.typesafe.ai/",
			"https://custom-proxy.internal:8443",
		}
		for _, u := range validURLs {
			c := defaultConfig()
			c.Jev.BaseURL = u
			if err := c.Validate(); err != nil {
				t.Fatalf("url %q 应通过: %v", u, err)
			}
		}
	})

	t.Run("Mode 校验", func(t *testing.T) {
		c := defaultConfig()
		c.Jev.Mode = "invalid_mode"
		var validation *ValidationError
		if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev.mode" {
			t.Fatalf("非法 mode 必须拒绝: %v", err)
		}
	})

	t.Run("Retries 边界", func(t *testing.T) {
		for _, r := range []int{-1, 2, 5} {
			c := defaultConfig()
			c.Jev.Retries = r
			var validation *ValidationError
			if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev.retries" {
				t.Fatalf("retries %d 应被拒绝: %v", r, err)
			}
		}
		for _, r := range []int{0, 1} {
			c := defaultConfig()
			c.Jev.Retries = r
			if err := c.Validate(); err != nil {
				t.Fatalf("retries %d 应通过: %v", r, err)
			}
		}
	})

	t.Run("Plane Timeout 与 Concurrency 边界", func(t *testing.T) {
		planes := []struct {
			name string
			set  func(c *Config, timeout time.Duration, concurrency int)
		}{
			{"notification", func(c *Config, t time.Duration, n int) {
				c.Jev.Notification.Timeout = t
				c.Jev.Notification.MaxConcurrency = n
			}},
			{"review", func(c *Config, t time.Duration, n int) { c.Jev.Review.Timeout = t; c.Jev.Review.MaxConcurrency = n }},
			{"chatops", func(c *Config, t time.Duration, n int) { c.Jev.ChatOps.Timeout = t; c.Jev.ChatOps.MaxConcurrency = n }},
		}

		for _, p := range planes {
			// Timeout < 1ms 或 > 1000ms
			for _, d := range []time.Duration{0, 1001 * time.Millisecond} {
				c := defaultConfig()
				p.set(&c, d, 8)
				var validation *ValidationError
				if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev."+p.name+".timeout" {
					t.Fatalf("plane %s timeout %v 应被拒绝: %v", p.name, d, err)
				}
			}
			// Concurrency < 1 或 > 32
			for _, n := range []int{0, 33} {
				c := defaultConfig()
				p.set(&c, 500*time.Millisecond, n)
				var validation *ValidationError
				if err := c.Validate(); !errors.As(err, &validation) || validation.Field != "jev."+p.name+".max_concurrency" {
					t.Fatalf("plane %s concurrency %d 应被拒绝: %v", p.name, n, err)
				}
			}
		}
	})
}
