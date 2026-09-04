package cli

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestRunner版本别名支持(t *testing.T) {
	for _, alias := range []string{"version", "-v", "--version"} {
		var stdout, stderr bytes.Buffer
		runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
			BuildInfo: func() buildinfo.Info {
				return buildinfo.Info{Version: "1.2.3"}
			},
		})
		if err := runner.Run(t.Context(), []string{alias}); err != nil {
			t.Fatalf("alias %q 运行失败: %v", alias, err)
		}
		if !strings.Contains(stdout.String(), "1.2.3") {
			t.Fatalf("alias %q 输出不含版本号: %s", alias, stdout.String())
		}
	}
}

func TestRunner帮助命令提示(t *testing.T) {
	for _, alias := range []string{"help", "-h", "--help"} {
		var stdout, stderr bytes.Buffer
		runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{})
		if err := runner.Run(t.Context(), []string{alias}); err != nil {
			t.Fatalf("alias %q 输出帮助不应失败: %v", alias, err)
		}
		if !strings.Contains(stdout.String(), "可用命令: serve") {
			t.Fatalf("alias %q 应向 stdout 输出可用命令列表: %s", alias, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("alias %q 不应向 stderr 输出帮助: %s", alias, stderr.String())
		}
	}
}

func TestRunner版本输出完整构建信息(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		BuildInfo: func() buildinfo.Info {
			return buildinfo.Info{
				Version:      "0.1.0",
				GitSHA:       "abc123",
				GitBranch:    "feature/test",
				BuildTime:    "2026-07-27T12:00:00Z",
				BuildChannel: "test",
				GoVersion:    "go1.26.4",
			}
		},
	})
	if err := runner.Run(t.Context(), []string{"version"}); err != nil {
		t.Fatalf("version 失败: %v", err)
	}
	for _, expected := range []string{"0.1.0", "abc123", "feature/test", "2026-07-27T12:00:00Z", "test", "go1.26.4"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("version 输出缺少 %q: %s", expected, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr=%q，期望为空", stderr.String())
	}
}

func TestRunner配置校验输出简短安全摘要(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	secret := "never-print-this-encryption-key"
	password := "never-print-this-admin-password"
	databaseURL := "postgres://admin:never-print-db-password@example/repo"
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		LoadConfig: func(_ context.Context, options config.LoadOptions) (config.Config, error) {
			if options.ConfigPath != "configs/test.yaml" {
				t.Fatalf("ConfigPath=%q", options.ConfigPath)
			}
			return config.Config{
				HTTP:     config.HTTPConfig{Addr: "127.0.0.1:8080"},
				Database: config.DatabaseConfig{Driver: "postgres", URL: databaseURL},
				Admin: config.AdminBootstrapConfig{
					Username: "Repo Admin",
					Password: config.NewSecret(password),
				},
				Encryption: config.EncryptionConfig{CurrentKey: config.NewSecret(secret)},
				Setup:      config.SetupConfig{AllowRemote: false},
			}, nil
		},
	})
	if err := runner.Run(t.Context(), []string{"config", "validate", "--config", "configs/test.yaml"}); err != nil {
		t.Fatalf("config validate 失败: %v", err)
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "config_valid") || !strings.Contains(output, "driver=postgres") {
		t.Fatalf("配置摘要缺少安全状态: %s", output)
	}
	for _, forbidden := range []string{secret, password, databaseURL, "never-print-db-password"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("配置摘要泄漏敏感值 %q: %s", forbidden, output)
		}
	}
}

func TestRunner无效配置返回稳定错误码(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, &config.ValidationError{Field: "database.driver", Message: "must be sqlite or postgres"}
		},
	})
	if err := runner.Run(t.Context(), []string{"config", "validate"}); err == nil {
		t.Fatal("无效配置应返回错误")
	}
	if !strings.Contains(stderr.String(), "error_code=validation_failed") {
		t.Fatalf("stderr 缺少 validation_failed: %s", stderr.String())
	}
}

func TestRunner管理员重置只从Stdin读取一行且不回显(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	password := "新的管理员密码一二三四五六"
	var received string
	runner := NewRunner(strings.NewReader(password+"\nignored second line\n"), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, nil
		},
		ResetAdminPassword: func(_ context.Context, _ config.Config, candidate string) error {
			received = candidate
			return nil
		},
	})
	if err := runner.Run(t.Context(), []string{"admin", "reset-password", "--password-stdin"}); err != nil {
		t.Fatalf("reset-password 失败: %v", err)
	}
	if received != password {
		t.Fatalf("传给重置服务的密码=%q，期望仅第一行", received)
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, password) || strings.Contains(output, "ignored second line") {
		t.Fatalf("reset-password 输出泄漏密码: %s", output)
	}
	if !strings.Contains(stdout.String(), "所有旧 Session 已撤销") {
		t.Fatalf("成功输出缺少 Session 撤销说明: %s", stdout.String())
	}
}

func TestRunner拒绝命令行密码参数且不泄漏参数值(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var called atomic.Bool
	secret := "命令行绝密密码一二三四五六"
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		ResetAdminPassword: func(context.Context, config.Config, string) error {
			called.Store(true)
			return nil
		},
	})
	if err := runner.Run(t.Context(), []string{"admin", "reset-password", "--password", secret}); err == nil {
		t.Fatal("命令行密码参数应被拒绝")
	}
	if called.Load() {
		t.Fatal("拒绝命令行密码后不应调用重置服务")
	}
	if strings.Contains(stderr.String(), secret) {
		t.Fatalf("错误输出泄漏命令行密码: %s", stderr.String())
	}
}

func TestRunner数据库或密钥失败只输出安全诊断(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	secretCause := "postgres://admin:secret@example/repo"
	runner := NewRunner(strings.NewReader("新的管理员密码一二三四五六\n"), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, nil
		},
		ResetAdminPassword: func(context.Context, config.Config, string) error {
			return safeTestError{code: "database_unavailable", message: "无法打开数据库。", cause: secretCause}
		},
	})
	if err := runner.Run(t.Context(), []string{"admin", "reset-password", "--password-stdin"}); err == nil {
		t.Fatal("数据库失败应返回错误")
	}
	if !strings.Contains(stderr.String(), "error_code=database_unavailable") || strings.Contains(stderr.String(), secretCause) {
		t.Fatalf("数据库失败诊断不安全: %s", stderr.String())
	}
}

func TestRunnerServe装配并运行应用(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	application := &fakeApplication{}
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, nil
		},
		BuildApp: func(context.Context, config.Config) (Application, error) {
			return application, nil
		},
	})
	if err := runner.Run(t.Context(), []string{"serve"}); err != nil {
		t.Fatalf("serve 失败: %v", err)
	}
	if !application.ran.Load() || !application.closed.Load() {
		t.Fatalf("serve 生命周期=(ran:%v, closed:%v)，期望均为 true", application.ran.Load(), application.closed.Load())
	}
}

type fakeApplication struct {
	ran    atomic.Bool
	closed atomic.Bool
}

func (a *fakeApplication) Run(context.Context) error {
	a.ran.Store(true)
	return nil
}

func (a *fakeApplication) Close() error {
	a.closed.Store(true)
	return nil
}

type safeTestError struct {
	code    string
	message string
	cause   string
}

func (e safeTestError) Error() string         { return e.cause }
func (e safeTestError) ErrorCode() string     { return e.code }
func (e safeTestError) PublicMessage() string { return e.message }

var _ error = safeTestError{}

func TestRunnerAdminReset2FA(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var called atomic.Bool
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{}, nil
		},
		ResetAdmin2FA: func(context.Context, config.Config) error {
			called.Store(true)
			return nil
		},
	})
	if err := runner.Run(t.Context(), []string{"admin", "reset-2fa"}); err != nil {
		t.Fatalf("reset-2fa 运行失败: %v", err)
	}
	if !called.Load() {
		t.Fatal("未调用 ResetAdmin2FA")
	}
	if !strings.Contains(stdout.String(), "reset=ok 2fa_disabled=true sessions_revoked=true") {
		t.Fatalf("输出缺少 reset=ok 2fa_disabled=true: %s", stdout.String())
	}
}
