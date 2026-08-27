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

// TestWorkflowConclusionLabel 守护 workflow 结论中文标签映射：
// rules 通知与 digest 报告共用，任何一侧的用例回归都说明映射被改坏。
func TestWorkflowConclusionLabel(t *testing.T) {
	cases := []struct {
		conclusion, want string
	}{
		{"success", "成功"},
		{"failure", "失败"},
		{"startup_failure", "失败"},
		{"timed_out", "超时"},
		{"cancelled", "已取消"},
		{"action_required", "需处理"},
		{"skipped", "已跳过"},
		{"in_progress", "进行中"},
		{"queued", "进行中"},
		{"pending", "进行中"},
		{"unknown", "已完成"},
	}
	for _, tc := range cases {
		if got := WorkflowConclusionLabel(tc.conclusion); got != tc.want {
			t.Errorf("WorkflowConclusionLabel(%q) = %q, want %q", tc.conclusion, got, tc.want)
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

// TestChannelListCacheInvalidatedOnWrite 守护渠道列表缓存的写路径即时失效：
// 通知规则热路径走 List 缓存，渠道变更必须立即对后续 List 可见，不能等 TTL。
func TestChannelListCacheInvalidatedOnWrite(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	ch, err := st.Channels().Upsert(ctx, NotificationChannel{
		ChannelType: ChannelTelegram, Name: "ch-a", Enabled: true, Target: "123",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustList := func() []NotificationChannel {
		items, err := st.Channels().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	if got := len(mustList()); got != 1 {
		t.Fatalf("创建后应列出 1 条，got %d", got)
	}
	if err := st.Channels().ToggleEnabled(ctx, ch.ID, false); err != nil {
		t.Fatal(err)
	}
	if items := mustList(); len(items) == 0 || items[0].Enabled {
		t.Fatalf("停用后应立即对 List 可见，got %+v", items)
	}
	if err := st.Channels().Delete(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(mustList()); got != 0 {
		t.Fatalf("删除后应立即对 List 可见，got %d", got)
	}
}

// TestStarTrendWindowSeedsBeforeRange 守护窗口查询的前向补值语义：
// 仅有窗口前快照的仓必须以「窗口前最近一条」为种值从窗口首日参与求和，
// 与全量载入的曲线起点一致（否则 days>0 视图会整仓丢失）。
func TestStarTrendWindowSeedsBeforeRange(t *testing.T) {
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
	r1 := mkRepo("repo-w1", "w1")
	r2 := mkRepo("repo-w2", "w2")
	today := time.Now().UTC()
	// r1 唯一快照远在窗口（7 天）之外：应以种值 7 从窗口首日参与。
	// r2 快照在窗口内 3 天前：从其快照日起参与。
	if _, err := st.RepoStatSnapshots().Upsert(ctx, RepoStatSnapshot{
		RepositoryID: r1, Metric: MetricStargazers, Value: 7, SampleDate: today.AddDate(0, 0, -60).Format("2006-01-02"),
	}); err != nil {
		t.Fatal(err)
	}
	insideDate := today.AddDate(0, 0, -3).Format("2006-01-02")
	if _, err := st.RepoStatSnapshots().Upsert(ctx, RepoStatSnapshot{
		RepositoryID: r2, Metric: MetricStargazers, Value: 5, SampleDate: insideDate,
	}); err != nil {
		t.Fatal(err)
	}
	points, err := st.StarTrend(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	fromDate := today.AddDate(0, 0, -6).Format("2006-01-02")
	if len(points) != 7 || points[0].Date != fromDate {
		t.Fatalf("窗口应从 %s 起共 7 天，got %+v", fromDate, points)
	}
	// 首日 r2 尚无快照：仅 r1 种值参与。
	if points[0].Total != 7 {
		t.Fatalf("首日应仅含 r1 种值 7，got %d", points[0].Total)
	}
	for _, p := range points {
		if p.Date >= insideDate && p.Total != 12 {
			t.Fatalf("%s 起应为 r1 7 + r2 5 = 12，got %d", insideDate, p.Total)
		}
	}

	// 3 天窗口把 r2 快照也挤出窗外：两仓均以种值参与，合计恒为 12。
	seededOnly, err := st.StarTrend(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(seededOnly) != 3 {
		t.Fatalf("3 天窗口应返回 3 个点，got %+v", seededOnly)
	}
	for _, p := range seededOnly {
		if p.Total != 12 {
			t.Fatalf("窗口内无快照时种值合计应恒为 12，date %s got %d", p.Date, p.Total)
		}
	}
}

// TestStarTrendWindowSeedAndInRangeMerge 守护混合游标：同一仓既有窗口前种值又有窗口内
// 快照时，窗口首段用种值、快照日起切换为窗口值（种值与窗口快照不能重复计入）。
func TestStarTrendWindowSeedAndInRangeMerge(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.Repositories().Upsert(ctx, Repository{ID: "repo-mix", Type: RepositoryTypeInstallation,
		SyncStatus: SyncStatusActive, Owner: "o", Name: "mix", FullName: "o/mix",
		StarsEnabled: true, WatchesEnabled: true}); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC()
	for _, s := range []RepoStatSnapshot{
		{RepositoryID: "repo-mix", Metric: MetricStargazers, Value: 100, SampleDate: today.AddDate(0, 0, -60).Format("2006-01-02")},
		{RepositoryID: "repo-mix", Metric: MetricStargazers, Value: 130, SampleDate: today.AddDate(0, 0, -5).Format("2006-01-02")},
	} {
		if _, err := st.RepoStatSnapshots().Upsert(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	points, err := st.StarTrend(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	switchDate := today.AddDate(0, 0, -5).Format("2006-01-02")
	for _, p := range points {
		want := int64(100)
		if p.Date >= switchDate {
			want = 130
		}
		if p.Total != want {
			t.Fatalf("date %s: 快照日前用种值 100、日后用窗口值 130，got %d", p.Date, p.Total)
		}
	}
}

// TestRepoIDsCacheInvalidatedOnArchive 守护仓 ID 集合缓存的写路径即时失效：
// 归档操作后 TTL 窗口内再次列表必须立即排除该仓，不能读到归档前集合。
func TestRepoIDsCacheInvalidatedOnArchive(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	repo, err := st.Repositories().Upsert(ctx, Repository{ID: "repo-arc", Type: RepositoryTypeInstallation,
		SyncStatus: SyncStatusActive, Owner: "o", Name: "arc", FullName: "o/arc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.WorkItems().UpsertIfNewer(ctx, WorkItem{
		ID: "wi-arc-1", RepositoryID: repo.ID, Number: 1, Kind: WorkItemKindIssue, State: "open", Title: "t",
	}); err != nil {
		t.Fatal(err)
	}
	// 预热缓存：首次列表读出「含该仓」的活跃集合。
	items, _, err := st.WorkItems().List(ctx, ListFilter{Page: 1, PerPage: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("预热列表应含该仓工作项: %+v %v", items, err)
	}
	// 归档后立即列表：缓存必须已失效，不能读到归档前集合。
	archived := true
	if err := st.Repositories().UpdateSettings(ctx, repo.ID, RepositorySettings{IsArchived: &archived}); err != nil {
		t.Fatal(err)
	}
	items, _, err = st.WorkItems().List(ctx, ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("归档后工作项应立即从列表消失，got %+v", items)
	}
}

// TestOutboxRetryAllDead 守护批量重新排队：dead→pending 并清错误码与锁，
// pending 条目不受影响，渠道过滤只命中对应渠道。
func TestOutboxRetryAllDead(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	mk := func(id, ch, status string) {
		if _, err := st.Outbox().Create(ctx, NotificationOutbox{
			ID: id, ChannelID: ch, IdempotencyKey: "idem|" + id, Status: status,
			NextAttemptAt: time.Now().UTC().Add(time.Hour), LastErrorCode: "http_webhook_status_500",
			Title: "t", BodyText: "b",
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("ob-1", "ch-a", OutboxDead)
	mk("ob-2", "ch-b", OutboxDead)
	mk("ob-3", "ch-a", OutboxPending)

	// 渠道过滤：只重新排队 ch-a 的 dead。
	n, err := st.Outbox().RetryAllDead(ctx, []string{"ch-a"}, time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatalf("渠道过滤应重新排队 1 条: n=%d err=%v", n, err)
	}
	// 全量：剩余 ch-b 的 dead。
	n, err = st.Outbox().RetryAllDead(ctx, nil, time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatalf("全量应重新排队 1 条: n=%d err=%v", n, err)
	}
	// 终态核对：无 dead，三条全 pending；重新排队的错误码与锁已清。
	_, deadPage, err := st.Outbox().List(ctx, ListFilter{Page: 1, PerPage: 10, Status: OutboxDead})
	if err != nil || deadPage.Total != 0 {
		t.Fatalf("重排队后不应有 dead: total=%d err=%v", deadPage.Total, err)
	}
	items, pendingPage, err := st.Outbox().List(ctx, ListFilter{Page: 1, PerPage: 10, Status: OutboxPending})
	if err != nil || pendingPage.Total != 3 {
		t.Fatalf("应全部回到 pending: total=%d err=%v", pendingPage.Total, err)
	}
	for _, it := range items {
		if it.ID == "ob-1" || it.ID == "ob-2" {
			if it.LastErrorCode != "" || it.LockedUntil != nil {
				t.Fatalf("重排队后错误码与锁应清空: %+v", it)
			}
		}
	}
}
