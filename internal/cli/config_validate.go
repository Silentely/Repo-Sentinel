package cli

import (
	"context"
	"fmt"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func (r Runner) runConfigValidate(ctx context.Context, args []string) error {
	flags := newFlagSet("config validate")
	configPath := flags.String("config", "", "配置文件路径")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return newCLIError("config validate 参数不合法。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: *configPath})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		r.stdout,
		"config_valid driver=%s http_addr=%s setup_remote=%t admin_bootstrap=%t encryption_key_configured=%t\n",
		cfg.Database.Driver,
		cfg.HTTP.Addr,
		cfg.Setup.AllowRemote,
		cfg.Admin.Username != "",
		cfg.Encryption.CurrentKey.Reveal() != "",
	)
	return err
}
