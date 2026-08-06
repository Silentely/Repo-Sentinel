package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

// openTestStore 为 store 包内部测试建 sqlite 文件库（store_test 包另有一个同名辅助，互不冲突）。
func openTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(dir, "test.db") + "?_fk=1",
	})
	if err != nil {
		t.Fatalf("打开测试 Store 失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestKindDisplayName 守护领域层事件类型中文名（通知正文与 AI 分诊共用）。
func TestKindDisplayName(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{WorkItemKindIssue, "Issue"},
		{WorkItemKindPR, "PR"},
		{AlertKindDependabot, "Dependabot 依赖告警"},
		{AlertKindCodeScanning, "Code Scanning 代码扫描"},
		{AlertKindSecretScanning, "Secret Scanning 密钥扫描"},
		{"workflow_run", "Actions 工作流"},
		{"custom_kind", "custom_kind"},
	}
	for _, tc := range cases {
		if got := KindDisplayName(tc.kind); got != tc.want {
			t.Errorf("KindDisplayName(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// TestPayloadString 守护 PayloadSummary 字符串读取：类型不符、nil、缺失键均返回空。
func TestPayloadString(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42, "slice": []any{"a"}}
	if got := PayloadString(m, "key"); got != "value" {
		t.Errorf("PayloadString 期望 value，实际 %q", got)
	}
	for _, key := range []string{"num", "slice", "missing"} {
		if got := PayloadString(m, key); got != "" {
			t.Errorf("PayloadString(%q) 应返回空，实际 %q", key, got)
		}
	}
	if got := PayloadString(nil, "key"); got != "" {
		t.Errorf("nil map 应返回空，实际 %q", got)
	}
}

// TestRepoAllowsKindStarWatch 守护 star/watch 新事件类型的仓库级能力门禁。
// 与 Issues/PR/Actions/Alerts 一致：字段默认值由 Ent schema Default(true) 保证，
// 结构体字面量不补默认值，故"默认放行"在此用显式 true 表达。
func TestRepoAllowsKindStarWatch(t *testing.T) {
	base := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, StarsEnabled: true, WatchesEnabled: true}
	if !RepoAllowsKind(&base, StarKind) {
		t.Fatal("star kind should be allowed when StarsEnabled")
	}
	if !RepoAllowsKind(&base, WatchKind) {
		t.Fatal("watch kind should be allowed when WatchesEnabled")
	}
	off := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, StarsEnabled: false}
	if RepoAllowsKind(&off, StarKind) {
		t.Fatal("star kind should be gated by StarsEnabled")
	}
	offWatch := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, WatchesEnabled: false}
	if RepoAllowsKind(&offWatch, WatchKind) {
		t.Fatal("watch kind should be gated by WatchesEnabled")
	}
	archived := Repository{MonitorEnabled: true, SyncStatus: SyncStatusArchived}
	if RepoAllowsKind(&archived, StarKind) {
		t.Fatal("archived repo should not allow star kind")
	}
}

// TestIsSubscribableKindStarWatch 守护 star/watch 进入订阅白名单。
func TestIsSubscribableKindStarWatch(t *testing.T) {
	if !IsSubscribableKind(StarKind) || !IsSubscribableKind(WatchKind) {
		t.Fatal("star/watch should be subscribable")
	}
}

// TestRepoStatSnapshotUpsertIdempotent 守护快照按 (repository_id, metric, sample_date)
// 幂等：同日二次写入覆盖为新值，不产生重复行；ListInRange 含边界返回。
func TestRepoStatSnapshotUpsertIdempotent(t *testing.T) {
	st := openTestStore(t)
	repo, err := st.Repositories().Upsert(context.Background(), Repository{
		ID: newID(), Type: RepositoryTypeInstallation, SyncStatus: SyncStatusActive,
		Owner: "o", Name: "r", FullName: "o/r", StarsEnabled: true, WatchesEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snaps := st.RepoStatSnapshots()
	in := RepoStatSnapshot{RepositoryID: repo.ID, Metric: "stargazers", Value: 10, SampleDate: "2026-08-01"}
	if _, err := snaps.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	// 同日二次写入幂等覆盖为新值，不产生重复行。
	in.Value = 12
	if _, err := snaps.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	rows, err := snaps.ListInRange(ctx, []string{repo.ID}, "stargazers", "2026-08-01", "2026-08-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Value != 12 {
		t.Fatalf("want 1 row with value 12, got %+v", rows)
	}
}
