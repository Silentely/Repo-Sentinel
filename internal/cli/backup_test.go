package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	// 与 backup.go 相同：匿名导入注册 "sqlite" 驱动（modernc.org/sqlite）。
	_ "modernc.org/sqlite"
)

// sqliteTestRunner 构造仅注入 LoadConfig 的 Runner，沿用 run_test.go 的 buffer 风格。
// DatabaseConfig 由调用方按场景给出（sqlite 指向真实临时库，postgres 仅提供 URL）。
func sqliteTestRunner(stdout, stderr *bytes.Buffer, database config.DatabaseConfig) Runner {
	return NewRunner(strings.NewReader(""), stdout, stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			return config.Config{
				HTTP:     config.HTTPConfig{Addr: "127.0.0.1:8080"},
				Database: database,
			}, nil
		},
	})
}

// writeSQLiteFixtureDB 在临时路径建一个最小源库：单表 t、单行值 value，写完即关闭，
// 避免与后续 backup/restore 的连接竞争文件锁。
func writeSQLiteFixtureDB(t *testing.T, path, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("关闭测试库失败: %v", err)
		}
	}()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS t (v TEXT)"); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec("DELETE FROM t"); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t (v) VALUES (?)", value); err != nil {
		t.Fatalf("写入初始行失败: %v", err)
	}
}

// updateSQLiteFixtureDB 把 t 表唯一行的值改写为 value，用于制造 restore 前的脏数据。
func updateSQLiteFixtureDB(t *testing.T, path, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("关闭测试库失败: %v", err)
		}
	}()
	if _, err := db.Exec("UPDATE t SET v = ?", value); err != nil {
		t.Fatalf("改写测试库失败: %v", err)
	}
}

// readSQLiteFixtureValue 读回 t 表唯一行的值。
func readSQLiteFixtureValue(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("关闭测试库失败: %v", err)
		}
	}()
	var value string
	if err := db.QueryRow("SELECT v FROM t LIMIT 1").Scan(&value); err != nil {
		t.Fatalf("读回测试库失败: %v", err)
	}
	return value
}

func TestRunnerSQLite备份存在且只写stdout(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	writeSQLiteFixtureDB(t, dbPath, "a")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{Driver: "sqlite", URL: "file:" + dbPath})
	out := filepath.Join(dir, "backup.db")
	if err := runner.Run(t.Context(), []string{"backup", "--output", out}); err != nil {
		t.Fatalf("backup 失败: %v", err)
	}

	// 产物存在且非空：VACUUM INTO 必须真正落盘。
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("备份产物不存在: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("备份产物为空文件")
	}

	// 约定：子命令只返回错误，Runner.Run 统一打印；成功时 stdout 有 backup= 行、stderr 为空。
	if !strings.Contains(stdout.String(), "backup="+out) {
		t.Fatalf("stdout 缺少 backup= 行: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("backup stderr=%q，期望为空", stderr.String())
	}
}

func TestRunnerSQLite恢复回滚数据并清理WAL残留(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	writeSQLiteFixtureDB(t, dbPath, "a")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{Driver: "sqlite", URL: "file:" + dbPath})

	// 备份值为 'a' 的源库。
	bak := filepath.Join(dir, "backup.db")
	if err := runner.Run(t.Context(), []string{"backup", "--output", bak}); err != nil {
		t.Fatalf("backup 失败: %v", err)
	}

	// 制造脏数据 'b'，并手工放置陈旧 -wal/-shm 文件，模拟 WAL 模式下的残留。
	updateSQLiteFixtureDB(t, dbPath, "b")
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(dbPath+suffix, []byte("陈旧残留"), 0o644); err != nil {
			t.Fatalf("放置 %s 残留失败: %v", suffix, err)
		}
	}

	if err := runner.Run(t.Context(), []string{"restore", "--input", bak}); err != nil {
		t.Fatalf("restore 失败: %v", err)
	}

	// 先断言 -wal/-shm 已删除，再重新打开库读值（打开连接可能重建 WAL 文件，顺序不可颠倒）。
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("restore 后 %s 残留未被清理（err=%v）", dbPath+suffix, err)
		}
	}
	// 回滚语义：重新打开后值必须回到备份时的 'a'，而不是脏数据 'b'。
	if got := readSQLiteFixtureValue(t, dbPath); got != "a" {
		t.Fatalf("restore 后值=%q，期望回滚到 %q", got, "a")
	}

	// safety_copy 行给出恢复前的兜底副本路径，该文件必须真实存在，
	// 且作为 copyFile 新建文件权限应为 0600。
	output := stdout.String()
	if !strings.Contains(output, "restored="+dbPath) {
		t.Fatalf("stdout 缺少 restored= 行: %q", output)
	}
	var safetyPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "safety_copy=") {
			safetyPath = strings.TrimPrefix(line, "safety_copy=")
		}
	}
	if safetyPath == "" {
		t.Fatalf("stdout 缺少 safety_copy= 行: %q", output)
	}
	info, err := os.Stat(safetyPath)
	if err != nil {
		t.Fatalf("safety_copy 文件不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("safety_copy 权限=%o，期望 600", perm)
	}
	if stderr.Len() != 0 {
		t.Fatalf("restore stderr=%q，期望为空", stderr.String())
	}
}

func TestRunnerSQLite恢复失败时主库保持原状(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	writeSQLiteFixtureDB(t, dbPath, "a")
	// 手工放置 -wal 残留：restore 失败时旧日志同样不得被删除，否则主库丢数据。
	if err := os.WriteFile(dbPath+"-wal", []byte("未重放日志"), 0o644); err != nil {
		t.Fatalf("放置 -wal 失败: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{Driver: "sqlite", URL: "file:" + dbPath})

	// 备份文件不存在：暂存拷贝必然失败，此时主库与 WAL 都必须原封不动。
	err := runner.Run(t.Context(), []string{"restore", "--input", filepath.Join(dir, "missing.db")})
	if err == nil {
		t.Fatal("备份文件缺失时 restore 应返回错误")
	}
	// 顺序不可颠倒：打开 sqlite 连接会把头校验失败的伪造 -wal 当作损坏日志删除，
	// 必须先断言 restore 本身没有清理它。
	if _, statErr := os.Stat(dbPath + "-wal"); statErr != nil {
		t.Fatalf("restore 失败后 -wal 不应被删除: %v", statErr)
	}
	if got := readSQLiteFixtureValue(t, dbPath); got != "a" {
		t.Fatalf("restore 失败后主库值=%q，期望保持 %q", got, "a")
	}
	if _, statErr := os.Stat(dbPath + ".restore-tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("暂存文件应被清理（err=%v）", statErr)
	}
}

func TestRunner恢复缺少Input时错误只打印一次(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	// 参数校验先于 LoadConfig：若实现误把配置加载提前，这里会立即失败暴露。
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{
		LoadConfig: func(context.Context, config.LoadOptions) (config.Config, error) {
			t.Error("缺少 --input 时不应调用 LoadConfig")
			return config.Config{}, nil
		},
	})

	err := runner.Run(t.Context(), []string{"restore"})
	if err == nil {
		t.Fatal("缺少 --input 应返回错误")
	}

	// 回归点：子命令不再自行打印错误（backup.go/doctor.go 重构约定），
	// 由 Runner.Run → reportError 统一打印一次，stderr 中错误行不得重复。
	if got := strings.Count(stderr.String(), "必须提供 --input"); got != 1 {
		t.Fatalf("错误文本在 stderr 出现 %d 次，期望恰好 1 次: %q", got, stderr.String())
	}
	if got := strings.Count(stderr.String(), "error_code=validation_failed"); got != 1 {
		t.Fatalf("error_code 在 stderr 出现 %d 次，期望恰好 1 次: %q", got, stderr.String())
	}
}

func TestRunnerDoctorSQLite可用时输出通过(t *testing.T) {
	// Atlas 直接使用 sql.DB 时会在 TMPDIR 下取固定 SQLite 锁名，测试目录必须隔离
	// （与 internal/httpapi webhook fixture 的处理一致）。
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(dir, "doctor.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err := runner.Run(t.Context(), []string{"doctor"}); err != nil {
		t.Fatalf("doctor 失败: %v", err)
	}

	for _, expected := range []string{"database_open=true", "doctor=ok"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("doctor 输出缺少 %q: %q", expected, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("doctor stderr=%q，期望为空", stderr.String())
	}
}

func TestRunnerPostgres备份缺少pgDump时返回明确错误(t *testing.T) {
	// 置空 PATH 后 exec.LookPath("pg_dump") 确定性失败，无需真实 PostgreSQL。
	t.Setenv("PATH", "")
	databaseURL := "postgres://postgres:secret-password@db.example:5432/repo"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{Driver: "postgres", URL: databaseURL})
	err := runner.Run(t.Context(), []string{"backup", "--output", filepath.Join(t.TempDir(), "out.dump")})
	if err == nil {
		t.Fatal("缺少 pg_dump 时 backup 应返回错误")
	}
	if !strings.Contains(err.Error(), "未找到 pg_dump") {
		t.Fatalf("返回错误缺少 %q: %v", "未找到 pg_dump", err)
	}
	// 错误经 Runner.Run 统一打印一次，且不得泄漏数据库连接串。
	if got := strings.Count(stderr.String(), "未找到 pg_dump"); got != 1 {
		t.Fatalf("错误文本在 stderr 出现 %d 次，期望恰好 1 次: %q", got, stderr.String())
	}
	if strings.Contains(stderr.String(), databaseURL) || strings.Contains(stderr.String(), "secret-password") {
		t.Fatalf("stderr 泄漏数据库连接串: %q", stderr.String())
	}
}

func TestRunner恢复新建目标文件权限为0600(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	writeSQLiteFixtureDB(t, dbPath, "a")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := sqliteTestRunner(&stdout, &stderr, config.DatabaseConfig{Driver: "sqlite", URL: "file:" + dbPath})

	bak := filepath.Join(dir, "backup.db")
	if err := runner.Run(t.Context(), []string{"backup", "--output", bak}); err != nil {
		t.Fatalf("backup 失败: %v", err)
	}
	// 删除目标库：restore 走 copyFile 新建文件的路径，此时 0600 建文件权限必须生效。
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("删除目标库失败: %v", err)
	}

	if err := runner.Run(t.Context(), []string{"restore", "--input", bak}); err != nil {
		t.Fatalf("restore 失败: %v", err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("restore 后目标库不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("新建目标库权限=%o，期望 600", perm)
	}
	if got := readSQLiteFixtureValue(t, dbPath); got != "a" {
		t.Fatalf("restore 后值=%q，期望 %q", got, "a")
	}
}
