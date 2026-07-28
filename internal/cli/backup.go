package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	_ "modernc.org/sqlite"
)

func (r Runner) runBackup(ctx context.Context, args []string) error {
	fs := newFlagSet("backup")
	configPath := fs.String("config", "", "配置文件路径")
	output := fs.String("output", "", "备份输出路径")
	if err := fs.Parse(args); err != nil {
		return reportError(r.stderr, newCLIError("backup 参数无效。"))
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return reportError(r.stderr, err)
	}
	out := strings.TrimSpace(*output)
	if out == "" {
		out = fmt.Sprintf("reposentinel-backup-%s", time.Now().UTC().Format("20060102T150405Z"))
	}
	switch cfg.Database.Driver {
	case "sqlite":
		return r.backupSQLite(cfg.Database.URL, out)
	case "postgres":
		return r.backupPostgres(ctx, cfg.Database.URL, out)
	default:
		return reportError(r.stderr, newCLIError("不支持的数据库驱动。"))
	}
}

func (r Runner) backupSQLite(dbURL, out string) error {
	// file:./path.db -> path
	path := strings.TrimPrefix(dbURL, "file:")
	path = strings.Split(path, "?")[0]
	if path == "" {
		return reportError(r.stderr, newCLIError("无法解析 SQLite 路径。"))
	}
	// 使用驱动 VACUUM INTO，避免 shell 拼接路径。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return reportError(r.stderr, err)
	}
	defer db.Close()
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return reportError(r.stderr, err)
		}
	}
	if _, err := db.Exec("VACUUM INTO ?", out); err != nil {
		return reportError(r.stderr, fmt.Errorf("vacuum into failed: %w", err))
	}
	fmt.Fprintf(r.stdout, "backup=%s\n", out)
	fmt.Fprintf(r.stdout, "note=请同时备份 REPOSENTINEL_ENCRYPTION_KEY\n")
	return nil
}

func (r Runner) backupPostgres(ctx context.Context, dbURL, out string) error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return reportError(r.stderr, newCLIError("未找到 pg_dump，请安装 PostgreSQL 客户端工具。"))
	}
	if !strings.HasSuffix(out, ".dump") {
		out = out + ".dump"
	}
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--file="+out, dbURL)
	if b, err := cmd.CombinedOutput(); err != nil {
		return reportError(r.stderr, fmt.Errorf("pg_dump failed: %w (%s)", err, string(b)))
	}
	fmt.Fprintf(r.stdout, "backup=%s\n", out)
	fmt.Fprintf(r.stdout, "note=请同时备份 REPOSENTINEL_ENCRYPTION_KEY\n")
	return nil
}

func (r Runner) runRestore(ctx context.Context, args []string) error {
	fs := newFlagSet("restore")
	configPath := fs.String("config", "", "配置文件路径")
	input := fs.String("input", "", "备份文件路径")
	if err := fs.Parse(args); err != nil {
		return reportError(r.stderr, newCLIError("restore 参数无效。"))
	}
	in := strings.TrimSpace(*input)
	if in == "" {
		return reportError(r.stderr, newCLIError("必须提供 --input 备份文件。"))
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return reportError(r.stderr, err)
	}
	switch cfg.Database.Driver {
	case "sqlite":
		path := strings.TrimPrefix(cfg.Database.URL, "file:")
		path = strings.Split(path, "?")[0]
		// 先备份当前
		safety := path + ".pre-restore-" + time.Now().Format("20060102T150405")
		_ = copyFile(path, safety)
		if err := copyFile(in, path); err != nil {
			return reportError(r.stderr, err)
		}
		fmt.Fprintf(r.stdout, "restored=%s\n", path)
		fmt.Fprintf(r.stdout, "safety_copy=%s\n", safety)
		fmt.Fprintf(r.stdout, "note=请使用与备份匹配的 REPOSENTINEL_ENCRYPTION_KEY 启动并验证通知凭据可解密\n")
		return nil
	case "postgres":
		if _, err := exec.LookPath("pg_restore"); err != nil {
			return reportError(r.stderr, newCLIError("未找到 pg_restore。"))
		}
		cmd := exec.CommandContext(ctx, "pg_restore", "--clean", "--if-exists", "--dbname="+cfg.Database.URL, in)
		if b, err := cmd.CombinedOutput(); err != nil {
			return reportError(r.stderr, fmt.Errorf("pg_restore failed: %w (%s)", err, string(b)))
		}
		fmt.Fprintf(r.stdout, "restored=postgres\n")
		fmt.Fprintf(r.stdout, "note=请使用与备份匹配的 REPOSENTINEL_ENCRYPTION_KEY 启动并验证\n")
		return nil
	default:
		return reportError(r.stderr, newCLIError("不支持的数据库驱动。"))
	}
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o600)
}
