package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestOpen数据库连接错误不泄露凭据(t *testing.T) {
	secretURL := "postgres://sensitive-user:sensitive-password@127.0.0.1:1/reposentinel?sslmode=disable"
	_, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "postgres",
		URL:          secretURL,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err == nil {
		t.Fatal("连接不可用 PostgreSQL 时未返回错误")
	}
	for _, secret := range []string{secretURL, "sensitive-user", "sensitive-password"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("数据库错误泄露敏感文本 %q: %v", secret, err)
		}
	}
	if err.Error() != "database unavailable" {
		t.Fatalf("数据库错误=%q，期望固定安全文本 database unavailable", err)
	}
}

func TestOpen拒绝数据库Revision超过程序目录(t *testing.T) {
	databaseURL := "file:" + filepath.Join(t.TempDir(), "future-revision.db")
	cfg := config.DatabaseConfig{Driver: "sqlite", URL: databaseURL}
	opened, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("首次打开 Store 失败: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("关闭首次 Store 失败: %v", err)
	}

	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	// 插入一条高于程序目录的 revision，模拟「库比应用新」的降级场景。
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO atlas_schema_revisions (
			version, description, type, applied, total, executed_at, execution_time, hash
		) VALUES (
			'99999999999999', 'future', 2, 1, 1, datetime('now'), 0, 'test'
		)
	`); err != nil {
		_ = db.Close()
		t.Fatalf("写入未来 revision 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭测试数据库失败: %v", err)
	}

	_, err = store.Open(t.Context(), cfg)
	if err == nil || err.Error() != "database migration failed" {
		t.Fatalf("未来 revision 打开错误=%v，期望固定安全迁移错误", err)
	}
}
