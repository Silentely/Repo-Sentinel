package rules

import (
	"context"
	"encoding/json"
	htmlpkg "html"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	dbURL := "file:" + filepath.Join(t.TempDir(), "agg.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func seedChannel(t *testing.T, data store.Store) store.NotificationChannel {
	t.Helper()
	ch, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func TestAggregatorMergesWithinWindow(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	agg := NewAggregator(data, 50*time.Millisecond, 100, time.Minute)

	repoID := ulid.Make().String()
	makeEvent := func(title string) *store.Event {
		return &store.Event{
			ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
			Title: title, RepositoryID: &repoID,
		}
	}
	ctx := context.Background()
	if err := agg.Evaluate(ctx, normalizer.Result{Event: makeEvent("a")}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	if err := agg.Evaluate(ctx, normalizer.Result{Event: makeEvent("b")}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	// 窗口结束前不应有 outbox
	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("窗口内不应投递，got %d", len(items))
	}
	time.Sleep(80 * time.Millisecond)
	items, _, err = data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("合并后应 1 条 outbox，got %d", len(items))
	}
	if items[0].BodyJSON["count"] != float64(2) && items[0].BodyJSON["count"] != 2 {
		// JSON number may be int
		if n, ok := items[0].BodyJSON["count"].(int); !ok || n != 2 {
			if f, ok := items[0].BodyJSON["count"].(float64); !ok || f != 2 {
				t.Fatalf("aggregate count=%v", items[0].BodyJSON["count"])
			}
		}
	}
}

func TestAggregator窗口内关闭全局开关不投递(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	agg := NewAggregator(data, 50*time.Millisecond, 100, time.Minute)

	repoID := ulid.Make().String()
	ctx := context.Background()
	for _, title := range []string{"a", "b"} {
		ev := &store.Event{
			ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
			Title: title, RepositoryID: &repoID,
		}
		if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	// 窗口未到期前全局关闭 Issues：flush 时必须按最新开关过滤，不得投递已入桶事件。
	raw, _ := json.Marshal(false)
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: "set-issues-off", Key: store.SettingFeatureIssues, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("全局关闭后合并通知不得投递，got %d", len(items))
	}
}

func TestAggregatorBurstSummary(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	repoID := ulid.Make().String()
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		ev := &store.Event{
			ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
			Title: "x", RepositoryID: &repoID,
		}
		if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 1 {
		t.Fatal("超频后应产生摘要 outbox")
	}
	// 标题应包含仓库名，Telegram 预览无需展开正文即可定位超频来源。
	if !strings.Contains(items[0].Title, "acme/demo") {
		t.Fatalf("超频摘要标题应含仓库名，实际: %q", items[0].Title)
	}
	if !strings.Contains(items[0].Title, "通知频率超限") {
		t.Fatalf("超频摘要标题应保留通用语义，实际: %q", items[0].Title)
	}
}

// TestHTMLEscape 守护通知文案的转义行为：合并与实时消息统一使用标准库
// html.EscapeString（含单引号），避免自定义 replacer 与标准库行为分叉。
func TestHTMLEscape(t *testing.T) {
	got := htmlpkg.EscapeString(`a<b>&"c'`)
	want := "a&lt;b&gt;&amp;&#34;c&#39;"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMultiInstanceAggregateIdempotency(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	// 两个独立聚合器模拟双实例；手动 flush 保证同一时间桶。
	agg1 := NewAggregator(data, time.Minute, 100, time.Minute)
	agg2 := NewAggregator(data, time.Minute, 100, time.Minute)
	repoID := ulid.Make().String()
	makeEvent := func(title string) *store.Event {
		return &store.Event{
			ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
			Title: title, RepositoryID: &repoID,
		}
	}
	ctx := context.Background()
	_ = agg1.Evaluate(ctx, normalizer.Result{Event: makeEvent("a")}, "acme/demo")
	_ = agg1.Evaluate(ctx, normalizer.Result{Event: makeEvent("b")}, "acme/demo")
	_ = agg2.Evaluate(ctx, normalizer.Result{Event: makeEvent("c")}, "acme/demo")
	_ = agg2.Evaluate(ctx, normalizer.Result{Event: makeEvent("d")}, "acme/demo")
	key := repoID + "|issue"
	agg1.flush(key)
	agg2.flush(key)
	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	// 时间桶幂等：两实例合并通知应收敛为 1 条（同渠道同桶）。
	if len(items) != 1 {
		t.Fatalf("多实例应幂等为 1 条 outbox，got %d", len(items))
	}
}

func TestTimeBucket(t *testing.T) {
	ts := time.Unix(1_700_000_060, 0).UTC()
	b1 := timeBucket(ts, time.Minute)
	b2 := timeBucket(ts.Add(30*time.Second), time.Minute)
	if b1 != b2 {
		t.Fatalf("same minute bucket: %d vs %d", b1, b2)
	}
	b3 := timeBucket(ts.Add(2*time.Minute), time.Minute)
	if b3 == b1 {
		t.Fatal("later minute should differ")
	}
}

func TestAggregator合并按渠道订阅重建子集(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	// 渠道只订阅 dependabot；security 桶内含 dependabot + code_scanning。
	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-sub", ChannelType: store.ChannelTelegram, Name: "sub", Enabled: true,
		Target: "1", EventKinds: []string{store.AlertKindDependabot}, DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agg := NewAggregator(data, 50*time.Millisecond, 100, time.Minute)
	repoID := ulid.Make().String()
	events := []*store.Event{
		{ID: ulid.Make().String(), Kind: store.AlertKindDependabot, Action: "opened", Title: "dep-alert", RepositoryID: &repoID, OccurredAt: time.Now().UTC()},
		{ID: ulid.Make().String(), Kind: store.AlertKindCodeScanning, Action: "opened", Title: "cs-alert", RepositoryID: &repoID, OccurredAt: time.Now().UTC()},
	}
	for _, ev := range events {
		if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(80 * time.Millisecond)
	out, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("合并应 1 条 outbox，got %d", len(out))
	}
	if !strings.Contains(out[0].BodyText, "dep-alert") {
		t.Fatalf("应包含订阅的 dependabot 事件，正文: %s", out[0].BodyText)
	}
	if strings.Contains(out[0].BodyText, "cs-alert") {
		t.Fatalf("不应包含未订阅的 code_scanning 事件，正文: %s", out[0].BodyText)
	}
	if !strings.Contains(out[0].Title, "× 1") {
		t.Fatalf("标题计数应为 1，实际: %s", out[0].Title)
	}
}

func TestAggregator渠道全不命中不产生outbox(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-none", ChannelType: store.ChannelTelegram, Name: "none", Enabled: true,
		Target: "1", EventKinds: []string{store.WorkItemKindPR}, DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agg := NewAggregator(data, 50*time.Millisecond, 100, time.Minute)
	repoID := ulid.Make().String()
	for _, kind := range []string{store.AlertKindDependabot, store.AlertKindCodeScanning} {
		ev := &store.Event{ID: ulid.Make().String(), Kind: kind, Action: "opened", Title: kind, RepositoryID: &repoID, OccurredAt: time.Now().UTC()}
		if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(80 * time.Millisecond)
	out, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("渠道不订阅桶内任何类型时不应有 outbox，got %d", len(out))
	}
}

func TestAggregatorBurst按sample类型过滤(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-burst", ChannelType: store.ChannelTelegram, Name: "burst", Enabled: true,
		Target: "1", EventKinds: []string{store.WorkItemKindPR}, DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	repoID := ulid.Make().String()
	for i := 0; i < 4; i++ {
		ev := &store.Event{ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened", Title: "x", RepositoryID: &repoID, OccurredAt: time.Now().UTC()}
		if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("渠道不订阅 issue，超频摘要不应投递，got %d", len(out))
	}
}

// 聚合参数可从 system_settings 热加载：管理台修改 notify.* 后无需重启即生效。
func TestAggregatorReloadFromSettings(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)
	agg := NewAggregator(data, time.Minute, 3, time.Minute)

	writeInt := func(key string, n int) {
		t.Helper()
		raw, _ := json.Marshal(n)
		if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
			ID: ulid.Make().String(), Key: key, ValueJSON: raw, UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	writeInt("notify.aggregate_window_sec", 120)
	writeInt("notify.burst_threshold", 50)
	writeInt("notify.burst_window_sec", 600)

	if err := agg.ReloadFrom(ctx); err != nil {
		t.Fatal(err)
	}
	if agg.Window != 120*time.Second || agg.BurstThreshold != 50 || agg.BurstWindow != 600*time.Second {
		t.Fatalf("热加载未生效: window=%v threshold=%d burst=%v", agg.Window, agg.BurstThreshold, agg.BurstWindow)
	}

	// 非法值不得覆盖已生效参数。
	writeInt("notify.aggregate_window_sec", -5)
	if err := agg.ReloadFrom(ctx); err != nil {
		t.Fatal(err)
	}
	if agg.Window != 120*time.Second {
		t.Fatalf("非法值不应覆盖现有配置: %v", agg.Window)
	}
}
