package rules

import (
	"context"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
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

// renderMergedMessage 合并正文应包含批次时间，便于用户判断这批通知对应的合并窗口。
func TestRenderMergedMessageIncludesWindowEnd(t *testing.T) {
	ev := &store.Event{Kind: store.WorkItemKindIssue, Action: "opened", Title: "t1"}
	windowEnd := time.Date(2026, 8, 8, 12, 34, 0, 0, time.UTC)
	_, body := renderMergedMessage("acme/demo", "issue", []*store.Event{ev}, windowEnd)
	if !strings.Contains(body, "2026-08-08 12:34 UTC") {
		t.Fatalf("合并正文应包含窗口结束时间，实际: %s", body)
	}
	if !strings.Contains(body, "（已聚合）") {
		t.Fatalf("标题应带已聚合标记，实际: %s", body)
	}
}

// renderMergedMessage 零值时间不应输出时间行（保持向后兼容的纯列表格式）。
func TestRenderMergedMessageZeroWindowEndOmitsTime(t *testing.T) {
	ev := &store.Event{Kind: store.WorkItemKindIssue, Action: "opened", Title: "t1"}
	_, body := renderMergedMessage("acme/demo", "issue", []*store.Event{ev}, time.Time{})
	if strings.Contains(body, "⏰ 时间：") {
		t.Fatalf("零值时间不应输出时间行，实际: %s", body)
	}
}

// enqueueBurstSummary 超频摘要必须携带事件链接（Telegram inline 按钮）与时间行，
// 让用户可从摘要直接跳到原始事件。
func TestEnqueueBurstSummaryCarriesLinkAndTime(t *testing.T) {
	data := openTestStore(t)
	ch, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-burst", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "chat-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	sample := &store.Event{
		ID: "ev-1", Kind: store.WorkItemKindIssue, Action: "opened",
		HTMLURL: "https://github.com/acme/demo/issues/1",
	}
	a := NewAggregator(data, 60*time.Second, 15, 5*time.Minute)
	if _, _, err := a.enqueueBurstSummary(t.Context(), "repo-1", "acme/demo", "issue", "⚠️ 通知频率超限", sample); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 条 outbox，实际 %d", len(items))
	}
	if items[0].HTMLURL != "https://github.com/acme/demo/issues/1" {
		t.Fatalf("超频摘要应携带事件链接，实际 %q", items[0].HTMLURL)
	}
	if !strings.Contains(items[0].BodyText, "⏰ 时间：") {
		t.Fatalf("超频摘要正文应包含时间行，实际: %s", items[0].BodyText)
	}
}

func TestEnqueueBurstSummaryDefersDuringQuietHours(t *testing.T) {
	data := openTestStore(t)
	now := time.Now().UTC()
	ch, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-burst-quiet", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "chat-1",
		QuietHoursEnabled: true,
		QuietHoursStart:   now.Add(-time.Minute).Format("15:04"),
		QuietHoursEnd:     now.Add(2 * time.Minute).Format("15:04"),
		QuietHoursTZ:      "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	sample := &store.Event{ID: "ev-burst-quiet", Kind: store.WorkItemKindIssue, Action: "opened"}
	a := NewAggregator(data, time.Minute, 15, 5*time.Minute)
	if _, _, err := a.enqueueBurstSummary(t.Context(), "repo-1", "acme/demo", "issue", "⚠️ 通知频率超限", sample); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 条 outbox，实际 %d", len(items))
	}
	if !items[0].NextAttemptAt.After(now) {
		t.Fatalf("静默时段内的超频摘要应延迟，实际投递时间 %v，当前时间 %v", items[0].NextAttemptAt, now)
	}
}

func TestEnqueueMergedSecurityUsesMostUrgentEventForQuietHours(t *testing.T) {
	data := openTestStore(t)
	now := time.Now().UTC()
	ch, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-merged-security", ChannelType: store.ChannelTelegram, Name: "tg", Enabled: true, Target: "chat-1",
		QuietHoursEnabled: true,
		QuietHoursStart:   now.Add(-time.Minute).Format("15:04"),
		QuietHoursEnd:     now.Add(2 * time.Minute).Format("15:04"),
		QuietHoursTZ:      "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	first := &store.Event{ID: "ev-low", Kind: store.AlertKindCodeScanning, Severity: "low", Title: "低危告警"}
	critical := &store.Event{ID: "ev-critical", Kind: store.AlertKindCodeScanning, Severity: "critical", Title: "高危告警"}
	a := NewAggregator(data, time.Minute, 15, 5*time.Minute)
	b := &aggBucket{category: "security", repoID: "repo-1", repoName: "acme/demo", events: []*store.Event{first, critical}}
	if err := a.enqueueMerged(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 条 outbox，实际 %d", len(items))
	}
	if items[0].NextAttemptAt.After(now.Add(10 * time.Second)) {
		t.Fatalf("包含高危告警的安全聚合不应延迟，实际投递时间 %v，当前时间 %v", items[0].NextAttemptAt, now)
	}
}

// waitOutboxCount 轮询等待 outbox 达到期望数量（默认 5s 超时）。
// 聚合 flush 由 time.AfterFunc 异步触发，固定 sleep 在 CI 高负载 / -race 下
// 可能早于 flush 完成而误报 got 0，轮询可消除此类时序 flaky。
func waitOutboxCount(t *testing.T, ctx context.Context, data store.Store, want int) []store.NotificationOutbox {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == want {
			return items
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 outbox 数量 %d 超时，当前 %d", want, len(items))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitFlushWindow 等待聚合窗口结束且 flush 有机会执行，期间持续断言 outbox 数量为 0。
// 用于「不应投递」类断言：固定 sleep 若早于 flush 完成则只是假通过，无法真正验证
// flush 后的过滤行为，因此改为等待整个窗口周期并逐次复核。
func waitFlushWindow(t *testing.T, ctx context.Context, data store.Store, window time.Duration) {
	t.Helper()
	start := time.Now()
	minWait := window + 150*time.Millisecond
	deadline := start.Add(window + 5*time.Second)
	for {
		items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Fatalf("预期无 outbox，got %d", len(items))
		}
		if time.Since(start) >= minWait {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	// 窗口结束后合并为 1 条；flush 异步执行，轮询等待（消除高负载下的时序 flaky）
	items = waitOutboxCount(t, ctx, data, 1)
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
	// flush 异步执行：等待窗口结束并复核整个周期内均无投递（消除高负载下的时序 flaky）
	waitFlushWindow(t, ctx, data, 50*time.Millisecond)
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

func TestAggregatorSuppressesBotWorkItemsPerChannel(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	ignored, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-ignore-bots", ChannelType: store.ChannelTelegram, Name: "ignore", Enabled: true,
		Target: "1", IgnoreBots: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	notify, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-notify-bots", ChannelType: store.ChannelHTTPWebhook, Name: "notify", Enabled: true,
		Target: "https://example.test/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ignored

	repoID := ulid.Make().String()
	agg := NewAggregator(data, time.Minute, 100, time.Minute)
	for _, title := range []string{"bot-a", "bot-b"} {
		if err := agg.Evaluate(ctx, normalizer.Result{Event: &store.Event{
			ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
			Title: title, RepositoryID: &repoID, SenderIsBot: true,
		}}, "acme/demo"); err != nil {
			t.Fatal(err)
		}
	}
	agg.FlushAll()

	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ChannelID != notify.ID {
		t.Fatalf("机器人聚合通知应只投递到未屏蔽渠道，got %+v", items)
	}
}

func TestAggregatorBurstSuppressesBotWorkItemsPerChannel(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	ignored, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-ignore-burst", ChannelType: store.ChannelTelegram, Name: "ignore", Enabled: true,
		Target: "1", IgnoreBots: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	notify, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-notify-burst", ChannelType: store.ChannelHTTPWebhook, Name: "notify", Enabled: true,
		Target: "https://example.test/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ignored

	sample := &store.Event{ID: "bot-burst", Kind: store.WorkItemKindIssue, Action: "opened", SenderIsBot: true}
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	if _, _, err := agg.enqueueBurstSummary(ctx, "repo-1", "acme/demo", "issue", "burst", sample); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ChannelID != notify.ID {
		t.Fatalf("机器人 burst 摘要应只投递到未屏蔽渠道，got %+v", items)
	}
}

// TestAggregatorBurstLogsWarn 超频降级必须 Warn 留痕（repo/次数）：异常流量审计信号。
func TestAggregatorBurstLogsWarn(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	buf, logger := newRulesLogger(t)
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	agg.Logger = logger
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
	out := buf.String()
	if !strings.Contains(out, "burst summary enqueued") || !strings.Contains(out, "acme/demo") || !strings.Contains(out, "events_in_window=") {
		t.Fatalf("超频降级应 Warn 留痕，实际: %s", out)
	}
}

// TestAggregatorBurstWindowDedup 复现线上告警风暴（18 条安全告警、阈值 15）：
// 越过阈值后同窗口内的后续事件只计数，不得重复写摘要、也不得重复留痕，
// 确保「日志条数 = 实际入队条数」。
func TestAggregatorBurstWindowDedup(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	buf, logger := newRulesLogger(t)
	// 超频窗口取 1 小时并避开小时边界：保证整段风暴落在同一时间桶内，
	// 跨桶会被判定为两次独立超频，下面的重复投递断言就不再稳定。
	if d := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)); d < 2*time.Second {
		time.Sleep(d)
	}
	agg := NewAggregator(data, 300*time.Millisecond, 15, time.Hour)
	agg.Logger = logger
	repoID := ulid.Make().String()
	ctx := context.Background()
	send := func(prefix string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			ev := &store.Event{
				ID: ulid.Make().String(), Kind: store.AlertKindDependabot, Action: "created",
				Title: fmt.Sprintf("%s-%d", prefix, i+1), RepositoryID: &repoID,
			}
			if err := agg.Evaluate(ctx, normalizer.Result{Event: ev}, "acme/demo"); err != nil {
				t.Fatal(err)
			}
		}
	}

	send("alert", 18)
	// 前 15 条进聚合桶（窗口结束投递），第 16 条起触发超频摘要：共 2 条 outbox。
	out := waitOutboxCount(t, ctx, data, 2)
	var digest, burst bool
	for _, it := range out {
		digest = digest || strings.Contains(it.Title, "安全告警 × 15（已聚合）")
		burst = burst || strings.Contains(it.Title, "通知频率超限")
	}
	if !digest || !burst {
		t.Fatalf("应同时产出聚合通知与超频摘要，实际: %+v", out)
	}
	if got := strings.Count(buf.String(), `msg="burst summary enqueued"`); got != 1 {
		t.Fatalf("18 条告警只应留痕 1 次入队，实际 %d 次:\n%s", got, buf.String())
	}

	// 同一超频窗口内追加告警：不得新增 outbox，也不得新增留痕。
	send("late", 3)
	time.Sleep(500 * time.Millisecond)
	after, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("同窗口追加告警不应新增 outbox，实际 %d", len(after))
	}
	if got := strings.Count(buf.String(), `msg="burst summary enqueued"`); got != 1 {
		t.Fatalf("同窗口追加告警不应重复留痕，实际 %d 次:\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "burst summary skipped") {
		t.Fatalf("同窗口事件应在入队前被拦截，不应走到入队失败分支:\n%s", buf.String())
	}
}

// TestEnqueueBurstSummarySkipReason 无接收渠道时摘要不入队：必须带原因留痕，
// 不得记成已入队（否则运维按日志统计会高估超频降级量）。
func TestEnqueueBurstSummarySkipReason(t *testing.T) {
	data := openTestStore(t)
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-pr-only", ChannelType: store.ChannelTelegram, Name: "pr", Enabled: true,
		Target: "1", EventKinds: []string{store.WorkItemKindPR},
	})
	if err != nil {
		t.Fatal(err)
	}
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	sample := &store.Event{ID: "burst-nc", Kind: store.WorkItemKindIssue, Action: "opened"}
	enqueued, reason, err := agg.enqueueBurstSummary(t.Context(), "repo-nc", "acme/demo", "issue", "burst", sample)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued || reason != "no_channel" {
		t.Fatalf("无渠道应返回 no_channel，got enqueued=%v reason=%q", enqueued, reason)
	}

	// 同渠道同时间桶重复写入：第二次应被幂等收敛并标记 duplicate。
	if _, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: "ch-all", ChannelType: store.ChannelTelegram, Name: "all", Enabled: true, Target: "2",
	}); err != nil {
		t.Fatal(err)
	}
	if enqueued, reason, err := agg.enqueueBurstSummary(t.Context(), "repo-dup", "acme/demo", "issue", "burst", sample); err != nil || !enqueued || reason != "" {
		t.Fatalf("首次写入应成功，got enqueued=%v reason=%q err=%v", enqueued, reason, err)
	}
	if enqueued, reason, err := agg.enqueueBurstSummary(t.Context(), "repo-dup", "acme/demo", "issue", "burst", sample); err != nil || enqueued || reason != "duplicate" {
		t.Fatalf("同桶重复写入应返回 duplicate，got enqueued=%v reason=%q err=%v", enqueued, reason, err)
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

func TestAggregatorFlushAll(t *testing.T) {
	data := openTestStore(t)
	_ = seedChannel(t, data)
	agg := NewAggregator(data, 10*time.Minute, 100, 10*time.Minute)
	repoID := ulid.Make().String()
	ctx := context.Background()

	// 写入两个不同分类事件
	ev1 := &store.Event{
		ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
		Title: "issue 1", RepositoryID: &repoID,
	}
	ev2 := &store.Event{
		ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
		Title: "issue 2", RepositoryID: &repoID,
	}
	_ = agg.Evaluate(ctx, normalizer.Result{Event: ev1}, "acme/demo")
	_ = agg.Evaluate(ctx, normalizer.Result{Event: ev2}, "acme/demo")

	// 桶内尚未触发定时器
	agg.FlushAll()

	items, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("FlushAll 后应产生 1 条合并通知，实际 %d", len(items))
	}

	// 验证 nil store 安全返回 0
	val, err := readPositiveIntSetting(ctx, nil, "any.key")
	if err != nil || val != 0 {
		t.Fatalf("readPositiveIntSetting(nil) = (%d, %v), want (0, nil)", val, err)
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
	// flush 异步执行，轮询等待合并结果（消除高负载下的时序 flaky）
	out := waitOutboxCount(t, ctx, data, 1)
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
	// flush 异步执行：等待窗口结束并复核整个周期内均无投递（消除高负载下的时序 flaky）
	waitFlushWindow(t, ctx, data, 50*time.Millisecond)
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

// TestAggregatorReloadFromUnsetKeys 守护：notify.* 键从未设置（默认实例常态）时
// 热加载按「保留现值」语义返回 nil，不报错误（此前 ErrNotFound 透传产生假 Warn）。
func TestAggregatorReloadFromUnsetKeys(t *testing.T) {
	data := openTestStore(t)
	agg := NewAggregator(data, time.Minute, 3, time.Minute)
	if err := agg.ReloadFrom(t.Context()); err != nil {
		t.Fatalf("键未设置应保留现值不报错: %v", err)
	}
	if agg.Window != time.Minute || agg.BurstThreshold != 3 || agg.BurstWindow != time.Minute {
		t.Fatalf("未设置键不应改动现值: window=%v threshold=%d burst=%v", agg.Window, agg.BurstThreshold, agg.BurstWindow)
	}
}

func TestAggregatorReloadFromMalformedSettingKeepsValidSettings(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	agg := NewAggregator(data, time.Minute, 3, time.Minute)

	valid, _ := json.Marshal(120)
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: "notify.burst_threshold", ValueJSON: valid, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: "notify.aggregate_window_sec", ValueJSON: []byte(`"bad"`), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := agg.ReloadFrom(ctx); err != nil {
		t.Fatalf("非法设置不应阻断其它合法设置: %v", err)
	}
	if agg.BurstThreshold != 120 {
		t.Fatalf("合法设置应热生效，got threshold=%d", agg.BurstThreshold)
	}
	if agg.Window != time.Minute {
		t.Fatalf("非法设置不应覆盖现值，got window=%v", agg.Window)
	}
}

// TestAggregatorFlushBudget 守护：flush 预算下限 30s，AI 配置超时更高时随之放宽
// （与 webhook 直发路径同一语义，避免聚合回放被硬顶截断）。
func TestAggregatorFlushBudget(t *testing.T) {
	data := openTestStore(t)
	if got := (&Aggregator{Store: data}).flushBudget(); got != 30*time.Second {
		t.Fatalf("无 AI 时应为下限 30s，got %v", got)
	}
	if got := (&Aggregator{Store: data, AI: &ai.Client{Enabled: true, APIKey: "k", Timeout: 45 * time.Second}}).flushBudget(); got != 55*time.Second {
		t.Fatalf("AI 45s 超时应放宽为 55s，got %v", got)
	}
}

func TestRenderMergedMessageTitlePlainText(t *testing.T) {
	events := []*store.Event{
		{
			Kind:   store.WorkItemKindIssue,
			Action: "opened",
			Title:  "Bug & Fix <1>",
		},
	}
	title, body := renderMergedMessage("acme/core&ui", "issue", events, time.Now().UTC())
	// title 应为纯文本
	if !strings.Contains(title, "acme/core&ui") {
		t.Fatalf("title 应为纯文本，got: %s", title)
	}
	if strings.Contains(title, "&amp;") {
		t.Fatalf("title 不应转义，got: %s", title)
	}
	// body 必须转义
	if !strings.Contains(body, "acme/core&amp;ui") {
		t.Fatalf("body 头部必须转义，got: %s", body)
	}
	if !strings.Contains(body, "Bug &amp; Fix &lt;1&gt;") {
		t.Fatalf("body 列表项必须转义，got: %s", body)
	}
}
