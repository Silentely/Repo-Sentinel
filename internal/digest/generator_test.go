package digest

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func openDigestStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "digest.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func seedTelegram(t *testing.T, data store.Store) {
	t.Helper()
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1", DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunOnceSkipsBeforeLocalSendWindow(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	// 固定 UTC 时区 09:00，当前 08:00 应跳过
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})

	g := &Generator{Store: data}
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("窗口前不应发送，got %d", len(items))
	}
}

func TestRunOnceSendsWhenEventsExist(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})

	// 写入一条近 24h 事件
	_, err := data.Events().Create(t.Context(), store.Event{
		ID: ulid.Make().String(), Source: "test", Kind: store.WorkItemKindIssue, Action: "opened",
		Title: "hello", OccurredAt: time.Now().UTC(), DedupeFingerprint: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	g := &Generator{Store: data}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应发送 1 条摘要，got %d", len(items))
	}

	// 同日再次调用应幂等跳过
	if err := g.RunOnce(t.Context(), now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	items2, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 1 {
		t.Fatalf("同日不应重复发送，got %d", len(items2))
	}
}

// 全局功能关闭后，对应 kind 的存量事件不进入每日摘要。
func TestRunOnceFiltersDisabledFeatureEvents(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})
	rawOff, _ := json.Marshal(false)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: store.SettingFeatureActions, ValueJSON: rawOff, UpdatedBy: "test",
	})

	// 仅有 workflow_run 事件：全局 Actions 关闭后应视为无事件、不发送。
	_, err := data.Events().Create(t.Context(), store.Event{
		ID: ulid.Make().String(), Source: "test", Kind: "workflow_run", Action: "completed",
		Title: "质量守卫", WorkflowConclusion: "cancelled", OccurredAt: time.Now().UTC(),
		DedupeFingerprint: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	g := &Generator{Store: data}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("全局 Actions 关闭后仅有 workflow 事件不应发摘要，got %d", len(items))
	}
}

func TestRunOnceEmptyDefaultNoSend(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})
	g := &Generator{Store: data}
	now := time.Date(2026, 7, 28, 9, 10, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("无事件默认不发送，got %d", len(items))
	}
}

// reportGeneratedAt 页脚断言用固定生成时刻，保证测试确定性。
var reportGeneratedAt = time.Date(2026, 8, 8, 9, 5, 0, 0, time.UTC)

func TestBuildDigestBody_WithEvents(t *testing.T) {
	num1 := int64(8)
	num2 := int64(42)
	events := []store.Event{
		{Kind: store.WorkItemKindIssue, Action: "opened", Title: "[BUG] 脚本不停", SubjectNumber: &num1},
		{Kind: store.WorkItemKindPR, Action: "merged", Title: "feat: 新功能", SubjectNumber: &num2},
		{Kind: store.WorkItemKindIssue, Action: "closed", Title: "修复完成"},
		{Kind: store.AlertKindDependabot, Action: "opened", Title: "bump lodash"},
		{Kind: "workflow_run", Action: "completed", Title: "CI"},
	}
	body := buildReportBody("📊 每日摘要 2026-07-30", events, "过去 24 小时", nil, reportGeneratedAt)

	if !strings.Contains(body, "共 5 条事件") {
		t.Errorf("期望包含事件总数，实际: %s", body)
	}
	if !strings.Contains(body, "Issue × 2") {
		t.Errorf("期望 Issue × 2，实际: %s", body)
	}
	if !strings.Contains(body, "PR × 1") {
		t.Errorf("期望 PR × 1，实际: %s", body)
	}
	if !strings.Contains(body, "Dependabot 依赖告警 × 1") {
		t.Errorf("期望 Dependabot 依赖告警 × 1（复用 store.KindDisplayName），实际: %s", body)
	}
	if !strings.Contains(body, "#8") {
		t.Errorf("期望包含 #8，实际: %s", body)
	}
	if !strings.Contains(body, "#42") {
		t.Errorf("期望包含 #42，实际: %s", body)
	}
	if !strings.Contains(body, "[已打开]") {
		t.Errorf("期望预览含中文状态「已打开」，实际: %s", body)
	}
	if !strings.Contains(body, "[已合并]") {
		t.Errorf("期望预览含中文状态「已合并」，实际: %s", body)
	}
	if !strings.Contains(body, "[已关闭]") {
		t.Errorf("期望预览含中文状态「已关闭」，实际: %s", body)
	}
}

func TestBuildDigestBody_Empty(t *testing.T) {
	body := buildReportBody("📊 每日摘要 2026-07-30", nil, "过去 24 小时", nil, reportGeneratedAt)

	if !strings.Contains(body, "无新事件") {
		t.Errorf("期望无事件提示，实际: %s", body)
	}
	if !strings.Contains(body, "⏰ 生成时间：2026-08-08 09:05 UTC") {
		t.Errorf("空报告也应带生成时间页脚，实际: %s", body)
	}
}

func TestBuildDigestBody_ManyEvents(t *testing.T) {
	events := make([]store.Event, 10)
	for i := range events {
		events[i] = store.Event{Kind: store.WorkItemKindIssue, Action: "opened", Title: "test"}
	}
	body := buildReportBody("test", events, "过去 24 小时", nil, reportGeneratedAt)

	if !strings.Contains(body, "共 10 条事件") {
		t.Errorf("期望包含总数，实际: %s", body)
	}
	previewCount := strings.Count(body, "• ")
	if previewCount != 5 {
		t.Errorf("期望 5 条预览，实际: %d", previewCount)
	}
}

func TestBuildDigestBody_SortedByCount(t *testing.T) {
	events := []store.Event{
		{Kind: "workflow_run", Title: "a"},
		{Kind: "workflow_run", Title: "b"},
		{Kind: "workflow_run", Title: "c"},
		{Kind: store.WorkItemKindIssue, Title: "d"},
	}
	body := buildReportBody("test", events, "过去 24 小时", nil, reportGeneratedAt)

	idxWorkflow := strings.Index(body, "Actions")
	idxIssue := strings.Index(body, "Issue")
	if idxWorkflow > idxIssue {
		t.Error("Actions 应排在 Issue 前面")
	}
}

func TestKindEmoji(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{store.WorkItemKindIssue, "🐛"},
		{store.WorkItemKindPR, "🔀"},
		{store.AlertKindDependabot, "📦"},
		{store.AlertKindCodeScanning, "🔎"},
		{store.AlertKindSecretScanning, "🔑"},
		{"workflow_run", "⚙️"},
		{"unknown", "📋"},
	}
	for _, tc := range cases {
		if got := rules.KindEmoji(tc.kind); got != tc.want {
			t.Errorf("kind=%q: 期望 %s，实际 %s", tc.kind, tc.want, got)
		}
	}
}

// TestKindDisplayName 已由 internal/store/domain_test.go 守护（digest 直接复用
// store.KindDisplayName，不再维护本地副本，避免两处映射漂移）。

func TestRunOnce渠道关闭汇总时不投递(t *testing.T) {
	data := openDigestStore(t)
	// 渠道关闭每日汇总。
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1", DigestEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})
	_, err = data.Events().Create(t.Context(), store.Event{
		ID: ulid.Make().String(), Source: "test", Kind: store.WorkItemKindIssue, Action: "opened",
		Title: "hello", OccurredAt: time.Now().UTC(), DedupeFingerprint: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	g := &Generator{Store: data}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("digest_enabled=false 的渠道不应收到汇总，got %d", len(items))
	}
	// 发送日期仍应落账，避免之后开启订阅时当日补发。
	if _, err := data.Settings().Get(t.Context(), settingLastDigest); err != nil {
		t.Fatalf("last_sent_date 应已落账: %v", err)
	}
}

// aiStub 返回指向桩服务器的 AI 客户端，reply 为完整 HTTP 响应体。
func aiStub(t *testing.T, reply string) *ai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &ai.Client{BaseURL: srv.URL, APIKey: "sk-test", Enabled: true, DigestEnabled: true, TriageEnabled: true}
}

func seedUTCSettings(t *testing.T, data store.Store) {
	t.Helper()
	rawTZ, _ := json.Marshal("UTC")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})
}

func createIssueEvent(t *testing.T, data store.Store, title string) {
	t.Helper()
	_, err := data.Events().Create(t.Context(), store.Event{
		ID: ulid.Make().String(), Source: "test", Kind: store.WorkItemKindIssue, Action: "opened",
		Title: title, OccurredAt: time.Now().UTC(), DedupeFingerprint: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// AI 摘要成功时正文替换为 AI 总结，且 BodyJSON 标记 ai=true。
func TestRunOnceAISummary(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "hello")

	g := &Generator{Store: data, AI: aiStub(t, `{"choices":[{"message":{"content":"今日共 1 条事件：- 新 Issue hello"}}]}`)}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应发送 1 条摘要，got %d", len(items))
	}
	if !strings.Contains(items[0].BodyText, "今日共 1 条事件") {
		t.Fatalf("正文应为 AI 总结，实际: %s", items[0].BodyText)
	}
	if items[0].BodyJSON["ai"] != true {
		t.Fatalf("BodyJSON.ai 应为 true，实际: %v", items[0].BodyJSON["ai"])
	}
}

// AI 失败时回退模板正文，BodyJSON 标记 ai=false。
func TestRunOnceAIFallback(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "hello")

	g := &Generator{Store: data, AI: aiStub(t, `internal error`)}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应发送 1 条摘要，got %d", len(items))
	}
	if !strings.Contains(items[0].BodyText, "共 1 条事件") {
		t.Fatalf("AI 失败应回退模板，实际: %s", items[0].BodyText)
	}
	if items[0].BodyJSON["ai"] != false {
		t.Fatalf("BodyJSON.ai 应为 false，实际: %v", items[0].BodyJSON["ai"])
	}
}

// AI 输出含 HTML 特殊字符时必须被转义，避免破坏 Telegram HTML 解析。
func TestRunOnceAIEscapesHTML(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "hello")

	g := &Generator{Store: data, AI: aiStub(t, `{"choices":[{"message":{"content":"<b>bold</b> & <i>italic</i>"}}]}`)}
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应发送 1 条摘要，got %d", len(items))
	}
	if strings.Contains(items[0].BodyText, "<b>bold</b>") {
		t.Fatalf("AI 输出必须转义，实际: %s", items[0].BodyText)
	}
	if !strings.Contains(items[0].BodyText, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Fatalf("期望转义后的 HTML，实际: %s", items[0].BodyText)
	}
}

// 周报：周一发送、幂等、AI 回退。
func TestRunWeekly(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "weekly item")
	rawOn, _ := json.Marshal(true)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingWeeklyEnabled, ValueJSON: rawOn, UpdatedBy: "test",
	})

	g := &Generator{Store: data, AI: aiStub(t, `{"choices":[{"message":{"content":"本周共 1 条事件：- weekly item"}}]}`)}
	// 2026-07-27 是周一。
	now := time.Date(2026, 7, 27, 9, 15, 0, 0, time.UTC)
	if err := g.RunWeekly(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("周一应发送周报，got %d", len(items))
	}
	if !strings.HasPrefix(items[0].IdempotencyKey, "report|weekly|") {
		t.Fatalf("幂等键前缀应为 report|weekly，实际: %s", items[0].IdempotencyKey)
	}
	if !strings.Contains(items[0].BodyText, "本周共 1 条事件") {
		t.Fatalf("正文应为 AI 总结，实际: %s", items[0].BodyText)
	}

	// 同日再次调用幂等跳过。
	if err := g.RunWeekly(t.Context(), now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	items2, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 1 {
		t.Fatalf("同日不应重复发送周报，got %d", len(items2))
	}
}

// 周报：非发送日不发送；未启用不发送。
func TestRunWeeklySkips(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "weekly item")
	rawOn, _ := json.Marshal(true)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingWeeklyEnabled, ValueJSON: rawOn, UpdatedBy: "test",
	})

	g := &Generator{Store: data}
	// 2026-07-28 是周二，不应发送。
	now := time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC)
	if err := g.RunWeekly(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("非发送日不应发送周报，got %d", len(items))
	}

	// 未启用时同样不发送。
	rawOff, _ := json.Marshal(false)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingWeeklyEnabled, ValueJSON: rawOff, UpdatedBy: "test",
	})
	monday := time.Date(2026, 7, 27, 9, 15, 0, 0, time.UTC)
	if err := g.RunWeekly(t.Context(), monday); err != nil {
		t.Fatal(err)
	}
	items2, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 0 {
		t.Fatalf("未启用时不应发送周报，got %d", len(items2))
	}
}

// 月报：每月 1 日发送且幂等。
func TestRunMonthly(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "monthly item")
	rawOn, _ := json.Marshal(true)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingMonthlyEnabled, ValueJSON: rawOn, UpdatedBy: "test",
	})

	g := &Generator{Store: data}
	// 2026-08-01 是每月发送日。
	now := time.Date(2026, 8, 1, 9, 15, 0, 0, time.UTC)
	if err := g.RunMonthly(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("每月 1 日应发送月报，got %d", len(items))
	}
	if !strings.HasPrefix(items[0].IdempotencyKey, "report|monthly|") {
		t.Fatalf("幂等键前缀应为 report|monthly，实际: %s", items[0].IdempotencyKey)
	}

	if err := g.RunMonthly(t.Context(), now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	items2, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 1 {
		t.Fatalf("同月不应重复发送月报，got %d", len(items2))
	}
}

// 月报：非发送日不发送。
func TestRunMonthlySkipsWrongDay(t *testing.T) {
	data := openDigestStore(t)
	seedTelegram(t, data)
	seedUTCSettings(t, data)
	createIssueEvent(t, data, "monthly item")
	rawOn, _ := json.Marshal(true)
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingMonthlyEnabled, ValueJSON: rawOn, UpdatedBy: "test",
	})

	g := &Generator{Store: data}
	now := time.Date(2026, 8, 2, 9, 15, 0, 0, time.UTC)
	if err := g.RunMonthly(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("非发送日不应发送月报，got %d", len(items))
	}
}

// 周报自定义发送日解析。
func TestParseWeekday(t *testing.T) {
	cases := []struct {
		in   string
		want time.Weekday
		ok   bool
	}{
		{"monday", time.Monday, true},
		{"sunday", time.Sunday, true},
		{"friday", time.Friday, true},
		{"MONDAY", 0, false}, // 调用方已统一转小写
		{"funday", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseWeekday(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseWeekday(%q) = (%v,%v), want (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// buildReportBody 时段文案参数化。
func TestBuildReportBody_PeriodLabel(t *testing.T) {
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "t", SubjectNumber: &num}}
	body := buildReportBody("📊 每周报告", events, "过去 7 天", nil, reportGeneratedAt)
	if !strings.Contains(body, "过去 7 天共 1 条事件") {
		t.Errorf("期望时段文案参数化，实际: %s", body)
	}
	if !strings.Contains(body, "⏰ 生成时间：2026-08-08 09:05 UTC") {
		t.Errorf("非空报告应带生成时间页脚，实际: %s", body)
	}
}

// buildReportBody 预览行必须转义用户输入标题：ParseMode=HTML 下 <、& 会破坏消息或注入。
func TestBuildReportBody_EscapesEventTitle(t *testing.T) {
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: `修复 <b>加粗</b> & "引号" 问题`}}
	body := buildReportBody("📊 每日摘要 2026-08-07", events, "过去 24 小时", nil, reportGeneratedAt)
	if strings.Contains(body, "<b>加粗</b>") {
		t.Fatalf("标题不应原样输出 HTML，实际: %s", body)
	}
	if !strings.Contains(body, "&lt;b&gt;加粗&lt;/b&gt; &amp;") {
		t.Fatalf("标题应按 HTML 转义，实际: %s", body)
	}
}

// buildReportBody 空事件文案应带上周期，避免日/周/月报共用含糊的「期间」。
func TestBuildReportBody_EmptyUsesPeriod(t *testing.T) {
	body := buildReportBody("📊 月度报告 2026-08", nil, "过去 30 天", nil, reportGeneratedAt)
	if !strings.Contains(body, "📭 过去 30 天无新事件") {
		t.Fatalf("空事件文案应包含周期，实际: %s", body)
	}
}

// buildReportBody 预览行带仓库名：多仓用户靠 full_name 区分事件归属；
// 无 RepositoryID 的事件保持原格式（不带仓库前缀）。
func TestBuildReportBody_ShowsRepoName(t *testing.T) {
	repoID := "repo-1"
	num := int64(42)
	events := []store.Event{
		{Kind: store.WorkItemKindIssue, Action: "opened", Title: "修复登录 Bug", SubjectNumber: &num, RepositoryID: &repoID},
		{Kind: store.WorkItemKindIssue, Action: "closed", Title: "无仓库事件"},
	}
	body := buildReportBody("📊 每日摘要 2026-08-08", events, "过去 24 小时", map[string]string{"repo-1": "acme/demo"}, reportGeneratedAt)
	if !strings.Contains(body, "acme/demo#42 修复登录 Bug") {
		t.Fatalf("预览行应带仓库名 acme/demo#42，实际: %s", body)
	}
	if !strings.Contains(body, "• [已关闭] 无仓库事件") {
		t.Fatalf("无仓库事件应保持原格式（不带仓库前缀），实际: %s", body)
	}
}

// TestBuildReportBody_ReleaseRepoFallback 验证 star 追踪的 release 事件（无 RepositoryID）
// 经 PayloadSummary 回退补仓库名，与 AI 总结输入同源修复。
func TestBuildReportBody_ReleaseRepoFallback(t *testing.T) {
	num := int64(369624730)
	events := []store.Event{
		{Kind: store.ReleaseKind, Action: "published", Title: "v0.4.24 - xAI SuperGrok plan_type hotfix",
			SubjectNumber: &num,
			PayloadSummary: map[string]any{
				"tag_name": "v0.4.24", "repository": "kittors/CliRelay",
			}},
	}
	body := buildReportBody("📊 每日摘要 2026-08-08", events, "过去 24 小时", nil, reportGeneratedAt)
	if !strings.Contains(body, "kittors/CliRelay#369624730") {
		t.Fatalf("release 预览行应回退补仓库名，实际: %s", body)
	}
}

// TestBuildReportBody_StarTitleDedup 验证 star/watch 事件标题即仓库名时不再重复前缀，
// 避免「Silentely/eSIM-Tools（Silentely/eSIM-Tools）」式噪声行。
func TestBuildReportBody_StarTitleDedup(t *testing.T) {
	repoID := "repo-1"
	events := []store.Event{
		{Kind: store.StarKind, Action: "created", Title: "acme/demo", RepositoryID: &repoID},
	}
	body := buildReportBody("📊 每日摘要 2026-08-08", events, "过去 24 小时", map[string]string{"repo-1": "acme/demo"}, reportGeneratedAt)
	if !strings.Contains(body, "• [已收藏] acme/demo") {
		t.Fatalf("预览行应为「已收藏」+ 仓库名（不重复），实际: %s", body)
	}
	if strings.Count(body, "acme/demo") != 1 {
		t.Fatalf("仓库名应只出现一次，实际: %s", body)
	}
}

// 非法时区应回退 UTC 而非报错，且窗口判定不受影响。
func TestSendWindowInvalidTimezoneFallsBackToUTC(t *testing.T) {
	data := openDigestStore(t)
	rawTZ, _ := json.Marshal("Mars/Olympus")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingTimezone, ValueJSON: rawTZ, UpdatedBy: "test",
	})
	rawTime, _ := json.Marshal("09:00")
	_, _ = data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLocalTime, ValueJSON: rawTime, UpdatedBy: "test",
	})
	g := &Generator{Store: data}
	loc, ok := g.sendWindow(t.Context(), time.Date(2026, 7, 28, 9, 15, 0, 0, time.UTC))
	if !ok {
		t.Fatal("09:15 应在 09:00 发送窗口内")
	}
	if loc != time.UTC {
		t.Fatalf("非法时区应回退 UTC，实际: %v", loc)
	}
	// 窗口为发送时刻起一小时 [09:00, 10:00)：09:30 仍在窗口内，10:30 超出。
	if _, ok := g.sendWindow(t.Context(), time.Date(2026, 7, 28, 9, 30, 0, 0, time.UTC)); !ok {
		t.Fatal("09:30 应在发送窗口内")
	}
	if _, ok := g.sendWindow(t.Context(), time.Date(2026, 7, 28, 10, 30, 0, 0, time.UTC)); ok {
		t.Fatal("10:30 超出发送窗口")
	}
}

// newTestSlog 返回写入内存缓冲的 DEBUG 级 slog 记录器，供 AI 参与度日志断言。
func newTestSlog(t *testing.T) (*bytes.Buffer, *slog.Logger) {
	t.Helper()
	var buf bytes.Buffer
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelDebug)
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lv}))
	return &buf, logger
}

// reportBody 日志：AI 启用且总结成功 → digest ai used，正文为 AI 总结。
func TestReportBodyLogsAIUsed(t *testing.T) {
	buf, logger := newTestSlog(t)
	g := &Generator{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"今日共 1 条事件：- 新 Issue hello"}}]}`)}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	body, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt)
	if !aiUsed {
		t.Fatal("AI 总结成功应标记参与")
	}
	if !strings.Contains(body, "今日共 1 条事件") {
		t.Fatalf("期望 AI 总结正文，实际: %s", body)
	}
	if !strings.Contains(buf.String(), `msg="digest ai used"`) {
		t.Fatalf("期望 digest ai used 日志，实际: %s", buf.String())
	}
}

// reportBody 日志：AI 调用失败 → digest ai fallback（reason=ai_error），正文回退模板。
func TestReportBodyLogsAIFallback(t *testing.T) {
	buf, logger := newTestSlog(t)
	g := &Generator{Logger: logger, AI: aiStub(t, `not-json`)}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	body, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt)
	if aiUsed {
		t.Fatal("AI 失败不应标记参与")
	}
	if !strings.Contains(body, "共 1 条事件") {
		t.Fatalf("期望回退模板正文，实际: %s", body)
	}
	out := buf.String()
	if !strings.Contains(out, `msg="digest ai fallback"`) || !strings.Contains(out, "reason=ai_error") {
		t.Fatalf("期望 digest ai fallback 日志，实际: %s", out)
	}
}

// reportBody 日志：AI 未启用 → digest ai skipped（reason=ai_not_enabled）。
func TestReportBodyLogsAISkipped(t *testing.T) {
	buf, logger := newTestSlog(t)
	g := &Generator{Logger: logger}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	_, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt)
	if aiUsed {
		t.Fatal("AI 未启用不应标记参与")
	}
	out := buf.String()
	if !strings.Contains(out, `msg="digest ai skipped"`) || !strings.Contains(out, "reason=ai_not_enabled") {
		t.Fatalf("期望 digest ai skipped 日志，实际: %s", out)
	}
}

// reportBody 质量护栏：AI 输出过短 → digest ai fallback（reason=low_quality），正文回退模板。
func TestReportBodyLogsLowQuality(t *testing.T) {
	buf, logger := newTestSlog(t)
	g := &Generator{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"一切正常"}}]}`)}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	body, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt)
	if aiUsed {
		t.Fatal("低质输出不应标记参与")
	}
	if !strings.Contains(body, "共 1 条事件") {
		t.Fatalf("期望回退模板正文，实际: %s", body)
	}
	out := buf.String()
	if !strings.Contains(out, `msg="digest ai fallback"`) || !strings.Contains(out, "reason=low_quality") {
		t.Fatalf("期望 low_quality 回退日志，实际: %s", out)
	}
}

// reportBody 质量护栏：AI 输出复读模板预览头（「最近活动：」）→ 低质回退。
func TestReportBodyLogsLowQualityTemplateEcho(t *testing.T) {
	buf, logger := newTestSlog(t)
	g := &Generator{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"共 1 条事件\n最近活动：\n• [已打开] hello"}}]}`)}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	_, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt)
	if aiUsed {
		t.Fatal("复读模板的输出不应标记参与")
	}
	out := buf.String()
	if !strings.Contains(out, "reason=low_quality") {
		t.Fatalf("期望 low_quality 回退日志，实际: %s", out)
	}
}

// reportBody 与 ai 层日志携带同一 req_id：参与度日志与调用日志可按单次决策串联。
func TestReportBodyReqIDConsistent(t *testing.T) {
	buf, logger := newTestSlog(t)
	aiClient := aiStub(t, `{"choices":[{"message":{"content":"今日共 1 条事件：- 新 Issue hello，问题已定位正在修复"}}]}`)
	// 同一 Logger 注入 AI 客户端，使参与度日志与 ai 层调用日志写入同一缓冲。
	aiClient.Logger = logger
	g := &Generator{Logger: logger, AI: aiClient}
	num := int64(1)
	events := []store.Event{{Kind: store.WorkItemKindIssue, Action: "opened", Title: "hello", SubjectNumber: &num}}
	if _, aiUsed := g.reportBody(t.Context(), "📊 每日摘要 2026-08-06", events, "过去 24 小时", reportGeneratedAt); !aiUsed {
		t.Fatal("AI 总结成功应标记参与")
	}
	re := regexp.MustCompile(`req_id=([0-9a-f]+)`)
	ids := re.FindAllStringSubmatch(buf.String(), -1)
	if len(ids) < 2 {
		t.Fatalf("期望 digest 与 ai 层日志均携带 req_id，实际: %s", buf.String())
	}
	if ids[0][1] != ids[1][1] {
		t.Fatalf("digest 与 ai 层 req_id 应一致，实际: %s", buf.String())
	}
}
