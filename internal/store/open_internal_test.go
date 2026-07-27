package store

import (
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestSQLite打开强制连接池与Pragma(t *testing.T) {
	db, _, _, err := openDatabase(config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + t.TempDir() + "/pragmas.db",
		MaxOpenConns: 99,
		MaxIdleConns: 99,
	})
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭 SQLite 失败: %v", err)
		}
	})
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections=%d，期望 1", got)
	}

	checks := []struct {
		pragma string
		want   int
	}{
		{pragma: "foreign_keys", want: 1},
		{pragma: "busy_timeout", want: 5000},
		{pragma: "synchronous", want: 1},
	}
	for _, check := range checks {
		var got int
		if err := db.QueryRowContext(t.Context(), "PRAGMA "+check.pragma).Scan(&got); err != nil {
			t.Fatalf("读取 PRAGMA %s 失败: %v", check.pragma, err)
		}
		if got != check.want {
			t.Fatalf("PRAGMA %s=%d，期望 %d", check.pragma, got, check.want)
		}
	}
	var journalMode string
	if err := db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("读取 PRAGMA journal_mode 失败: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("PRAGMA journal_mode=%q，期望 wal", journalMode)
	}
}
