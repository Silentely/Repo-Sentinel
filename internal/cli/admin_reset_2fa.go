package cli

import (
	"context"
	"fmt"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func (r Runner) runAdminReset2FA(ctx context.Context, args []string) error {
	flags := newFlagSet("admin reset-2fa")
	configPath := flags.String("config", "", "配置文件路径")
	flags.StringVar(configPath, "c", "", "配置文件路径（简写）")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return newCLIError("reset-2fa 参数不合法。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: *configPath})
	if err != nil {
		return err
	}
	if err := r.dependencies.ResetAdmin2FA(ctx, cfg); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.stdout, "reset=ok 2fa_disabled=true"); err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.stdout, "note=管理员两步验证已重置，可使用用户名和密码直接登录。")
	return err
}
