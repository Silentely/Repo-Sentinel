package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// 约定：子命令只返回错误，由 Runner.Run 统一向 stderr 打印一次。
func (r Runner) runDoctor(ctx context.Context, args []string) error {
	fs := newFlagSet("doctor")
	configPath := fs.String("config", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return newCLIError("doctor 参数无效。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return err
	}
	fmt.Fprintf(r.stdout, "http_addr=%s\n", cfg.HTTP.Addr)
	fmt.Fprintf(r.stdout, "database_driver=%s\n", cfg.Database.Driver)
	fmt.Fprintf(r.stdout, "encryption_key_configured=%t\n", strings.TrimSpace(cfg.Encryption.CurrentKey.Reveal()) != "")
	fmt.Fprintf(r.stdout, "github_app_id=%d\n", cfg.GitHub.AppID)
	fmt.Fprintf(r.stdout, "github_private_key_path=%s\n", cfg.GitHub.PrivateKeyPath)
	if cfg.GitHub.PrivateKeyPath != "" {
		_, statErr := os.Stat(cfg.GitHub.PrivateKeyPath)
		readable := statErr == nil
		// 文件存在但内容损坏（非合法 PEM）时 doctor 应报 false，否则误导排查
		// 「配置了却调不通 GitHub」的问题。
		parseable := false
		if readable {
			raw, readErr := os.ReadFile(cfg.GitHub.PrivateKeyPath)
			if readErr == nil {
				parseable = githubx.ValidatePrivateKeyPEM(string(raw)) == nil
			}
		}
		fmt.Fprintf(r.stdout, "github_private_key_readable=%t\n", readable)
		fmt.Fprintf(r.stdout, "github_private_key_parseable=%t\n", parseable)
	}
	fmt.Fprintf(r.stdout, "webhook_secret_configured=%t\n", strings.TrimSpace(cfg.GitHub.WebhookSecret.Reveal()) != "")
	fmt.Fprintf(r.stdout, "telegram_configured=%t\n", strings.TrimSpace(cfg.Notify.Telegram.Token.Reveal()) != "" && cfg.Notify.Telegram.ChatID != "")

	data, err := store.Open(ctx, cfg.Database)
	if err != nil {
		fmt.Fprintf(r.stdout, "database_open=false\n")
		return err
	}
	defer data.Close()
	fmt.Fprintf(r.stdout, "database_open=true\n")
	// 管理台可写入 github.runtime_config；doctor 仅报告是否存在记录（不解密）。
	if setting, err := data.Settings().Get(ctx, githubx.RuntimeSettingKey); err == nil && len(setting.ValueJSON) > 0 {
		fmt.Fprintf(r.stdout, "github_runtime_config_in_db=true\n")
	} else {
		fmt.Fprintf(r.stdout, "github_runtime_config_in_db=false\n")
	}
	if _, err := data.Admins().GetOnly(ctx); err != nil {
		fmt.Fprintf(r.stdout, "admin_exists=false\n")
	} else {
		fmt.Fprintf(r.stdout, "admin_exists=true\n")
	}
	stats, err := data.Dashboard(ctx)
	if err == nil {
		fmt.Fprintf(r.stdout, "repos_active=%d\n", stats.ReposActive)
		fmt.Fprintf(r.stdout, "outbox_dead=%d\n", stats.OutboxDead)
	} else {
		// 诊断工具不应静默略过：统计查询失败本身即是重要诊断信号。
		// 错误单行化：底层错误可能含换行（Ent/driver 多行错误），不能撑破 key=value 行格式。
		replacer := strings.NewReplacer("\n", " ", "\r", " ")
		fmt.Fprintf(r.stdout, "dashboard_stats=error:%s\n", replacer.Replace(err.Error()))
	}
	fmt.Fprintf(r.stdout, "doctor=ok\n")
	return nil
}
