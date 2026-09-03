package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	_ "modernc.org/sqlite"
)

// 约定：子命令只返回错误，由 Runner.Run 统一向 stderr 打印一次。
func (r Runner) runBackup(ctx context.Context, args []string) error {
	fs := newFlagSet("backup")
	configPath := fs.String("config", "", "配置文件路径")
	fs.StringVar(configPath, "c", "", "配置文件路径（简写）")
	output := fs.String("output", "", "备份输出路径")
	fs.StringVar(output, "o", "", "备份输出路径（简写）")
	if err := fs.Parse(args); err != nil {
		return newCLIError("backup 参数无效。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return err
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
		return newCLIError("不支持的数据库驱动。")
	}
}

func (r Runner) backupSQLite(dbURL, out string) error {
	// file:./path.db -> path
	path := strings.TrimPrefix(dbURL, "file:")
	path = strings.Split(path, "?")[0]
	if path == "" {
		return newCLIError("无法解析 SQLite 路径。")
	}
	// 使用驱动 VACUUM INTO，避免 shell 拼接路径。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	startedAt := time.Now()
	if _, err := db.Exec("VACUUM INTO ?", out); err != nil {
		return fmt.Errorf("vacuum into failed: %w", err)
	}
	fmt.Fprintf(r.stdout, "backup=%s\n", out)
	// 产物体积与耗时：0 字节损坏备份与正常备份此前输出相同格式，无法区分。
	if info, err := os.Stat(out); err == nil {
		fmt.Fprintf(r.stdout, "size_bytes=%d duration_ms=%d\n", info.Size(), time.Since(startedAt).Milliseconds())
	}
	fmt.Fprintf(r.stdout, "note=请同时备份 REPOSENTINEL_ENCRYPTION_KEY\n")
	return nil
}

func (r Runner) backupPostgres(ctx context.Context, dbURL, out string) error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return newCLIError("未找到 pg_dump，请安装 PostgreSQL 客户端工具。")
	}
	if !strings.HasSuffix(out, ".dump") {
		out = out + ".dump"
	}
	startedAt := time.Now()
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--file="+out, dbURL)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_dump failed: %w (%s)", err, string(b))
	}
	fmt.Fprintf(r.stdout, "backup=%s\n", out)
	if info, err := os.Stat(out); err == nil {
		fmt.Fprintf(r.stdout, "size_bytes=%d duration_ms=%d\n", info.Size(), time.Since(startedAt).Milliseconds())
	}
	fmt.Fprintf(r.stdout, "note=请同时备份 REPOSENTINEL_ENCRYPTION_KEY\n")
	return nil
}

func (r Runner) runRestore(ctx context.Context, args []string) error {
	fs := newFlagSet("restore")
	configPath := fs.String("config", "", "配置文件路径")
	fs.StringVar(configPath, "c", "", "配置文件路径（简写）")
	input := fs.String("input", "", "备份文件路径")
	fs.StringVar(input, "i", "", "备份文件路径（简写）")
	if err := fs.Parse(args); err != nil {
		return newCLIError("restore 参数无效。")
	}
	in := strings.TrimSpace(*input)
	if in == "" {
		return newCLIError("必须提供 --input 备份文件。")
	}
	cfg, err := r.dependencies.LoadConfig(ctx, config.LoadOptions{ConfigPath: strings.TrimSpace(*configPath)})
	if err != nil {
		return err
	}
	switch cfg.Database.Driver {
	case "sqlite":
		path := strings.TrimPrefix(cfg.Database.URL, "file:")
		path = strings.Split(path, "?")[0]
		// 先备份当前库；源库存在但副本失败时中止恢复，宁可不动主库。
		safety := path + ".pre-restore-" + time.Now().Format("20060102T150405")
		if err := copyFile(path, safety); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("安全副本创建失败，已中止恢复: %w", err)
			}
			safety = ""
		}
		// 先写同目录暂存文件再 rename 原子替换：直接截断式覆盖一旦中途失败，
		// 主库会变成空文件且 WAL 已被清理，只能手工从安全副本救回。
		tmp := path + ".restore-tmp"
		if err := copyFile(in, tmp); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("备份文件写入暂存失败，主库未改动: %w", err)
		}
		// 替换前校验文件头：SQLite 库以 "SQLite format 3\000" 开头，误传其它文件
		// （如 PostgreSQL dump 或任意文件）时中止，避免主库被替换成垃圾。
		if !isSQLiteFile(tmp) {
			_ = os.Remove(tmp)
			return newCLIError("备份文件不是有效的 SQLite 数据库，已中止恢复（主库未改动）。")
		}
		// WAL 模式下替换主文件前必须清理旧日志，否则残留 -wal 会被重放到新库上。
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				_ = os.Remove(tmp)
				return fmt.Errorf("清理 %s 失败: %w", path+suffix, err)
			}
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("原子替换主库失败: %w", err)
		}
		fmt.Fprintf(r.stdout, "restored=%s\n", path)
		if safety != "" {
			fmt.Fprintf(r.stdout, "safety_copy=%s\n", safety)
		}
		fmt.Fprintf(r.stdout, "note=restore 请在服务停止后执行；启动前确认 REPOSENTINEL_ENCRYPTION_KEY 与备份匹配\n")
		return nil
	case "postgres":
		if _, err := exec.LookPath("pg_restore"); err != nil {
			return newCLIError("未找到 pg_restore。")
		}
		cmd := exec.CommandContext(ctx, "pg_restore", "--clean", "--if-exists", "--dbname="+cfg.Database.URL, in)
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pg_restore failed: %w (%s)", err, string(b))
		}
		fmt.Fprintf(r.stdout, "restored=postgres\n")
		fmt.Fprintf(r.stdout, "note=restore 请在服务停止后执行；启动前确认 REPOSENTINEL_ENCRYPTION_KEY 与备份匹配\n")
		return nil
	default:
		return newCLIError("不支持的数据库驱动。")
	}
}

// isSQLiteFile 读取文件头 16 字节校验 SQLite 魔数（"SQLite format 3\000"）。
func isSQLiteFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var header [16]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return false
	}
	return string(header[:]) == "SQLite format 3\x00"
}

// copyFile 流式拷贝，避免大库一次性载入内存；目标统一收紧为 0600（覆盖既有文件时也生效）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}
