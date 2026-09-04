package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/Silentely/Repo-Sentinel/internal/app"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

const cliUsageMessage = "可用命令: serve, version, config validate, admin reset-password, admin reset-2fa, doctor, healthcheck, backup, restore。"

// Application 是 serve 子命令需要的最小运行时接口。
type Application interface {
	Run(context.Context) error
	Close() error
}

// Dependencies 提供 CLI 可注入的配置、应用、恢复与构建信息边界。
type Dependencies struct {
	LoadConfig         func(context.Context, config.LoadOptions) (config.Config, error)
	BuildApp           func(context.Context, config.Config) (Application, error)
	ResetAdminPassword func(context.Context, config.Config, string) error
	ResetAdmin2FA      func(context.Context, config.Config) error
	BuildInfo          func() buildinfo.Info
}

// Runner 绑定测试友好的标准流与依赖。
type Runner struct {
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	dependencies Dependencies
}

// NewRunner 创建 CLI Runner，并为未注入依赖补齐生产默认值。
func NewRunner(stdin io.Reader, stdout, stderr io.Writer, dependencies Dependencies) Runner {
	if stdin == nil {
		stdin = nilReader{}
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if dependencies.LoadConfig == nil {
		dependencies.LoadConfig = config.Load
	}
	if dependencies.BuildApp == nil {
		dependencies.BuildApp = func(ctx context.Context, cfg config.Config) (Application, error) {
			return app.Build(ctx, cfg)
		}
	}
	if dependencies.ResetAdminPassword == nil {
		dependencies.ResetAdminPassword = app.ResetAdminPassword
	}
	if dependencies.ResetAdmin2FA == nil {
		dependencies.ResetAdmin2FA = app.ResetAdmin2FA
	}
	if dependencies.BuildInfo == nil {
		dependencies.BuildInfo = buildinfo.Current
	}
	return Runner{stdin: stdin, stdout: stdout, stderr: stderr, dependencies: dependencies}
}

// Run 分派 serve/version/config/admin/doctor/healthcheck/backup/restore 子命令，并统一输出安全错误。
func (r Runner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return reportError(r.stderr, newCLIError("缺少命令，"+cliUsageMessage))
	}
	var err error
	switch args[0] {
	case "serve":
		err = r.runServe(ctx, args[1:])
	case "version", "-v", "--version":
		err = r.runVersion(args[1:])
	case "help", "-h", "--help":
		_, err = fmt.Fprintln(r.stdout, cliUsageMessage)
	case "config":
		if len(args) < 2 || args[1] != "validate" {
			err = newCLIError("config 仅支持 validate 子命令。")
		} else {
			err = r.runConfigValidate(ctx, args[2:])
		}
	case "admin":
		if len(args) < 2 {
			err = newCLIError("admin 仅支持 reset-password 与 reset-2fa 子命令。")
		} else {
			switch args[1] {
			case "reset-password":
				err = r.runAdminResetPassword(ctx, args[2:])
			case "reset-2fa":
				err = r.runAdminReset2FA(ctx, args[2:])
			default:
				err = newCLIError("admin 仅支持 reset-password 与 reset-2fa 子命令。")
			}
		}
	case "doctor":
		err = r.runDoctor(ctx, args[1:])
	case "healthcheck":
		err = r.runHealthcheck(ctx, args[1:])
	case "backup":
		err = r.runBackup(ctx, args[1:])
	case "restore":
		err = r.runRestore(ctx, args[1:])
	default:
		err = newCLIError("未知命令。")
	}
	if err != nil {
		return reportError(r.stderr, err)
	}
	return nil
}

func (r Runner) runServe(ctx context.Context, args []string) error {
	flags := newFlagSet("serve")
	configPath := flags.String("config", "", "配置文件路径")
	flags.StringVar(configPath, "c", "", "配置文件路径（简写）")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return newCLIError("serve 参数不合法。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: *configPath})
	if err != nil {
		return err
	}
	built, err := r.dependencies.BuildApp(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = built.Close() }()
	return built.Run(ctx)
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

type nilReader struct{}

func (nilReader) Read([]byte) (int, error) { return 0, io.EOF }
