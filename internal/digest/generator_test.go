package digest

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
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

func TestBuildDigestBody_WithEvents(t *testing.T) {
	num1 := 8
	num2 := 42
	events := []store.Event{
		{Kind: store.WorkItemKindIssue, Action: "opened", Title: "[BUG] 脚本不停", SubjectNumber: &num1},
		{Kind: store.WorkItemKindPR, Action: "merged", Title: "feat: 新功能", SubjectNumber: &num2},
		{Kind: store.WorkItemKindIssue, Action: "closed", Title: "修复完成"},
		{Kind: store.AlertKindDependabot, Action: "opened", Title: "bump lodash"},
		{Kind: "workflow_run", Action: "completed", Title: "CI"},
	}
	body := buildDigestBody("📊 每日摘要 2026-07-30", events)

	if !strings.Contains(body, "共 5 条事件") {
		t.Errorf("期望包含事件总数，实际: %s", body)
	}
	if !strings.Contains(body, "Issue × 2") {
		t.Errorf("期望 Issue × 2，实际: %s", body)
	}
	if !strings.Contains(body, "PR × 1") {
		t.Errorf("期望 PR × 1，实际: %s", body)
	}
	if !strings.Contains(body, "Dependabot × 1") {
		t.Errorf("期望 Dependabot × 1，实际: %s", body)
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
	body := buildDigestBody("📊 每日摘要 2026-07-30", nil)

	if !strings.Contains(body, "无新事件") {
		t.Errorf("期望无事件提示，实际: %s", body)
	}
}

func TestBuildDigestBody_ManyEvents(t *testing.T) {
	events := make([]store.Event, 10)
	for i := range events {
		events[i] = store.Event{Kind: store.WorkItemKindIssue, Action: "opened", Title: "test"}
	}
	body := buildDigestBody("test", events)

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
	body := buildDigestBody("test", events)

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
		if got := kindEmoji(tc.kind); got != tc.want {
			t.Errorf("kind=%q: 期望 %s，实际 %s", tc.kind, tc.want, got)
		}
	}
}

func TestKindDisplayName(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{store.WorkItemKindIssue, "Issue"},
		{store.WorkItemKindPR, "PR"},
		{store.AlertKindDependabot, "Dependabot"},
		{store.AlertKindCodeScanning, "Code Scanning"},
		{store.AlertKindSecretScanning, "Secret Scanning"},
		{"workflow_run", "Actions"},
		{"custom_kind", "custom_kind"},
	}
	for _, tc := range cases {
		if got := kindDisplayName(tc.kind); got != tc.want {
			t.Errorf("kind=%q: 期望 %s，实际 %s", tc.kind, tc.want, got)
		}
	}
}

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
