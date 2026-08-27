package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

const maxPasswordInputBytes = 8 * 1024

func (r Runner) runAdminResetPassword(ctx context.Context, args []string) error {
	flags := newFlagSet("admin reset-password")
	configPath := flags.String("config", "", "配置文件路径")
	passwordStdin := flags.Bool("password-stdin", false, "从 stdin 读取一行密码")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return newCLIError("reset-password 参数不合法；密码不得作为命令行参数。")
	}
	if !*passwordStdin {
		return newCLIError("非交互模式必须使用 --password-stdin。")
	}
	password, err := readPasswordLine(r.stdin)
	if err != nil {
		return err
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: *configPath})
	if err != nil {
		return err
	}
	if err := r.dependencies.ResetAdminPassword(ctx, cfg, password); err != nil {
		return err
	}
	// key=value 与 doctor/version/backup 输出风格一致，脚本化消费有落点。
	fmt.Fprintln(r.stdout, "reset=ok sessions_revoked=true")
	_, err = fmt.Fprintln(r.stdout, "note=管理员密码已重置，所有旧 Session 已撤销。")
	return err
}

func readPasswordLine(stdin io.Reader) (string, error) {
	reader := bufio.NewReader(io.LimitReader(stdin, maxPasswordInputBytes+1))
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", newCLIError("无法从 stdin 读取密码。")
	}
	if len(line) > maxPasswordInputBytes {
		return "", newCLIError("stdin 密码超过允许长度。")
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", newCLIError("stdin 密码不能为空。")
	}
	return line, nil
}
