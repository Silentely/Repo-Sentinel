package store

import "testing"

// TestSQLiteLockNameSameURL 同一库 URL 必须派生同一锁名，保证进程内互斥。
func TestSQLiteLockNameSameURL(t *testing.T) {
	a := sqliteLockName("file:/data/reposentinel.db")
	b := sqliteLockName("file:/data/reposentinel.db")
	if a != b {
		t.Fatalf("同一库 URL 应得到同一锁名，got %q vs %q", a, b)
	}
	if a == migrationLockName {
		t.Fatalf("锁名不应等于裸常量 %q", migrationLockName)
	}
}

// TestSQLiteLockNameDifferentURL 不同库 URL 必须派生不同锁名，
// 避免 go test ./... 并行包（独立进程、各自从 1 起）争用同一锁文件。
func TestSQLiteLockNameDifferentURL(t *testing.T) {
	seen := map[string]bool{}
	urls := []string{
		"file:/tmp/a.db",
		"file:/tmp/b.db",
		"file:/data/reposentinel.db",
		"file:/data/reposentinel_test_1.db",
		"file:repo.db",
		"file:digest.db",
	}
	for _, u := range urls {
		name := sqliteLockName(u)
		if seen[name] {
			t.Fatalf("不同库 URL %q 撞锁名 %q", u, name)
		}
		seen[name] = true
	}
}
