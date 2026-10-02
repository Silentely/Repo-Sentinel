package cli

import (
	"context"
	"fmt"

	"github.com/Silentely/Repo-Sentinel/internal/app"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func (r Runner) runSecretRotateKeys(ctx context.Context, args []string) error {
	flags := newFlagSet("secret rotate-keys")
	configPath := flags.String("config", "", "配置文件路径")
	flags.StringVar(configPath, "c", "", "配置文件路径（简写）")
	dryRun := flags.Bool("dry-run", false, "仅做只读演练，不修改数据库")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return newCLIError("rotate-keys 参数不合法。")
	}

	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: *configPath})
	if err != nil {
		return err
	}

	stats, err := r.dependencies.RotateKeys(ctx, cfg, app.RotateKeysOptions{DryRun: *dryRun})
	if err != nil {
		return err
	}

	mode := "applied"
	if *dryRun {
		mode = "dry_run"
	}

	if _, err := fmt.Fprintf(
		r.stdout,
		"status=ok mode=%s channels_rotated=%d settings_rotated=%d already_current=%d total_scanned=%d\n",
		mode,
		stats.ChannelsRotated,
		stats.SettingsRotated,
		stats.AlreadyCurrent,
		stats.ChannelsScanned+stats.SettingsScanned,
	); err != nil {
		return err
	}

	if *dryRun {
		_, err = fmt.Fprintln(r.stdout, "note=预检完成，数据库未发生更改。请在移除旧密钥前执行不带 --dry-run 的命令完成重加密。")
	} else if stats.ChannelsRotated == 0 && stats.SettingsRotated == 0 {
		_, err = fmt.Fprintln(r.stdout, "note=所有凭据已为当前最新密钥加密，无需变更。")
	} else {
		_, err = fmt.Fprintln(r.stdout, "note=密钥轮换完成，存量机密已全部迁移至当前主密钥。")
	}
	return err
}
