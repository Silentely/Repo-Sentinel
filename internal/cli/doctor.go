package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func (r Runner) runDoctor(ctx context.Context, args []string) error {
	fs := newFlagSet("doctor")
	configPath := fs.String("config", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return reportError(r.stderr, newCLIError("doctor 参数无效。"))
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return reportError(r.stderr, err)
	}
	fmt.Fprintf(r.stdout, "http_addr=%s\n", cfg.HTTP.Addr)
	fmt.Fprintf(r.stdout, "database_driver=%s\n", cfg.Database.Driver)
	fmt.Fprintf(r.stdout, "encryption_key_configured=%t\n", strings.TrimSpace(cfg.Encryption.CurrentKey.Reveal()) != "")
	fmt.Fprintf(r.stdout, "github_app_id=%d\n", cfg.GitHub.AppID)
	fmt.Fprintf(r.stdout, "github_private_key_path=%s\n", cfg.GitHub.PrivateKeyPath)
	if cfg.GitHub.PrivateKeyPath != "" {
		_, err := os.Stat(cfg.GitHub.PrivateKeyPath)
		fmt.Fprintf(r.stdout, "github_private_key_readable=%t\n", err == nil)
	}
	fmt.Fprintf(r.stdout, "webhook_secret_configured=%t\n", strings.TrimSpace(cfg.GitHub.WebhookSecret.Reveal()) != "")
	fmt.Fprintf(r.stdout, "telegram_configured=%t\n", strings.TrimSpace(cfg.Notify.Telegram.Token.Reveal()) != "" && cfg.Notify.Telegram.ChatID != "")

	data, err := store.Open(ctx, cfg.Database)
	if err != nil {
		fmt.Fprintf(r.stdout, "database_open=false\n")
		return reportError(r.stderr, err)
	}
	defer data.Close()
	fmt.Fprintf(r.stdout, "database_open=true\n")
	// 管理台可写入 github.runtime_config；doctor 仅报告是否存在记录（不解密）。
	if setting, err := data.Settings().Get(ctx, "github.runtime_config"); err == nil && len(setting.ValueJSON) > 0 {
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
	}
	fmt.Fprintf(r.stdout, "doctor=ok\n")
	return nil
}
