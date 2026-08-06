package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

// TestStarTrendForwardFillAndSum 守护 star 趋势聚合的逐日向前补值与跨仓求和：
// r1: 08-01=10, 08-03=15（08-02 缺，应沿用 10）；r2: 08-02=5（08-01 缺，从 08-02 起参与）。
// 期望：08-01=10, 08-02=15(r1 补 10 + r2 5), 08-03=20(r1 15 + r2 补 5)，此后无新快照，
// 两仓沿用末值，总数恒为 20 直至今天。范围终点为"今天"（UTC），故不断言固定长度，
// 只断言序列性质与关键日期合计，避免 CI 在测试数据日期之后运行即脆断。
func TestStarTrendForwardFillAndSum(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	mkRepo := func(id, full string) string {
		if _, err := st.Repositories().Upsert(ctx, Repository{ID: id, Type: RepositoryTypeInstallation,
			SyncStatus: SyncStatusActive, Owner: "o", Name: full, FullName: "o/" + full,
			StarsEnabled: true, WatchesEnabled: true}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	r1 := mkRepo("repo-1", "a")
	r2 := mkRepo("repo-2", "b")
	snaps := st.RepoStatSnapshots()
	for _, in := range []RepoStatSnapshot{
		{RepositoryID: r1, Metric: MetricStargazers, Value: 10, SampleDate: "2026-08-01"},
		{RepositoryID: r1, Metric: MetricStargazers, Value: 15, SampleDate: "2026-08-03"},
		{RepositoryID: r2, Metric: MetricStargazers, Value: 5, SampleDate: "2026-08-02"},
	} {
		if _, err := snaps.Upsert(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	points, err := st.StarTrend(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 || points[0].Date != "2026-08-01" {
		t.Fatalf("want first point on 2026-08-01, got %+v", points)
	}
	// 日期必须严格升序，终点为今天。
	for i := 1; i < len(points); i++ {
		prev, _ := time.Parse("2006-01-02", points[i-1].Date)
		cur, _ := time.Parse("2006-01-02", points[i].Date)
		if !cur.After(prev) {
			t.Fatalf("dates not ascending at %d: %+v", i, points)
		}
	}
	last, _ := time.Parse("2006-01-02", points[len(points)-1].Date)
	if today := time.Now().UTC().Format("2006-01-02"); last.Format("2006-01-02") != today {
		t.Fatalf("want last point on today %s, got %+v", today, points[len(points)-1])
	}
	// 关键日期的补值与求和。
	want := map[string]int64{"2026-08-01": 10, "2026-08-02": 15, "2026-08-03": 20}
	for _, p := range points {
		if total, ok := want[p.Date]; ok && p.Total != total {
			t.Fatalf("date %s: total %d, want %d", p.Date, p.Total, total)
		}
	}
	// 最后一个快照（08-03）之后无新数据，总数恒为 20。
	for _, p := range points {
		if p.Date > "2026-08-03" && p.Total != 20 {
			t.Fatalf("date %s: 快照后应沿用末值合计 20，got %d", p.Date, p.Total)
		}
	}
}
