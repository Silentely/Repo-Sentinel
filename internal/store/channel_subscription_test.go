package store_test

import (
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestNotificationChannelSubscriptionRoundTrip(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	// 子集订阅往返 + 关闭每日汇总。
	saved, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1", EventKinds: []string{store.WorkItemKindIssue, store.WorkItemKindPR},
		DigestEnabled: false,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := data.Channels().Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.EventKinds) != 2 || got.EventKinds[0] != store.WorkItemKindIssue || got.EventKinds[1] != store.WorkItemKindPR {
		t.Fatalf("EventKinds 往返失败: %+v", got.EventKinds)
	}
	if got.DigestEnabled {
		t.Fatal("DigestEnabled 应为 false")
	}

	// 未设置 EventKinds 时保持 nil=订阅全部；DigestEnabled=true。
	all, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelHTTPWebhook, Name: "hook",
		Enabled: true, Target: "https://example.com", DigestEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert all: %v", err)
	}
	gotAll, err := data.Channels().Get(ctx, all.ID)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if gotAll.EventKinds != nil {
		t.Fatalf("未设置 EventKinds 应保持 nil=全部: %+v", gotAll.EventKinds)
	}
	if !gotAll.DigestEnabled {
		t.Fatal("DigestEnabled 应为 true")
	}
}

func TestToggleEnabled保留订阅配置(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	saved, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1", EventKinds: []string{store.WorkItemKindIssue},
		DigestEnabled: false,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := data.Channels().ToggleEnabled(ctx, saved.ID, false); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	got, err := data.Channels().Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled {
		t.Fatal("toggle 后应为禁用")
	}
	if len(got.EventKinds) != 1 || got.EventKinds[0] != store.WorkItemKindIssue {
		t.Fatalf("toggle 不应改动订阅类型: %+v", got.EventKinds)
	}
	if got.DigestEnabled {
		t.Fatal("toggle 不应改动每日汇总开关")
	}
}

func TestAcceptsKind三态(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
		kind  string
		want  bool
	}{
		{"nil 接收全部", nil, "workflow_run", true},
		{"空数组拒收全部", []string{}, "issue", false},
		{"命中的类型", []string{"issue"}, "issue", true},
		{"未命中的类型", []string{"issue"}, "dependabot", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (store.NotificationChannel{EventKinds: tc.kinds}).AcceptsKind(tc.kind); got != tc.want {
				t.Fatalf("AcceptsKind=%v，期望 %v", got, tc.want)
			}
		})
	}
	if !store.IsSubscribableKind(store.AlertKindDependabot) || store.IsSubscribableKind("no_such_kind") {
		t.Fatal("IsSubscribableKind 白名单判定错误")
	}
}

func TestNotificationChannelGetByType(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	// 1. 无渠道时返回 ErrNotFound
	_, err := data.Channels().GetByType(ctx, store.ChannelFeishu)
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. 插入一个禁用的渠道
	disabledCh, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          ulid.Make().String(),
		ChannelType: store.ChannelFeishu,
		Name:        "feishu-disabled",
		Enabled:     false,
		Target:      "https://open.feishu.cn/hook/1",
	})
	if err != nil {
		t.Fatalf("upsert disabled: %v", err)
	}

	// 此时 GetEnabledByType 应返回 ErrNotFound，但 GetByType 应能找到
	if _, err := data.Channels().GetEnabledByType(ctx, store.ChannelFeishu); err != store.ErrNotFound {
		t.Fatalf("GetEnabledByType expected ErrNotFound, got %v", err)
	}
	gotDisabled, err := data.Channels().GetByType(ctx, store.ChannelFeishu)
	if err != nil || gotDisabled.ID != disabledCh.ID {
		t.Fatalf("GetByType failed: %v", err)
	}

	// 3. 插入一个启用的渠道，GetByType 应优先返回启用的
	enabledCh, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          ulid.Make().String(),
		ChannelType: store.ChannelFeishu,
		Name:        "feishu-enabled",
		Enabled:     true,
		Target:      "https://open.feishu.cn/hook/2",
	})
	if err != nil {
		t.Fatalf("upsert enabled: %v", err)
	}

	gotEnabled, err := data.Channels().GetByType(ctx, store.ChannelFeishu)
	if err != nil || gotEnabled.ID != enabledCh.ID {
		t.Fatalf("GetByType expected enabled channel ID %s, got %s (err: %v)", enabledCh.ID, gotEnabled.ID, err)
	}
}

func TestNotificationChannelPeriodicAndQuietHoursRoundTrip(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                   ulid.Make().String(),
		ChannelType:          store.ChannelSlack,
		Name:                 "slack-ops",
		Enabled:              true,
		Target:               "https://hooks.slack.com/services/T/B/X",
		ReceiveDailyDigest:   true,
		ReceiveWeeklyReport:  false,
		ReceiveMonthlyReport: true,
		QuietHoursEnabled:    true,
		QuietHoursStart:      "23:00",
		QuietHoursEnd:        "07:30",
		QuietHoursTZ:         "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("upsert slack channel: %v", err)
	}

	got, err := data.Channels().Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("get slack channel: %v", err)
	}
	if !got.ReceiveDailyDigest || got.ReceiveWeeklyReport || !got.ReceiveMonthlyReport {
		t.Fatalf("unexpected periodic flags: daily=%v, weekly=%v, monthly=%v",
			got.ReceiveDailyDigest, got.ReceiveWeeklyReport, got.ReceiveMonthlyReport)
	}
	if !got.QuietHoursEnabled || got.QuietHoursStart != "23:00" || got.QuietHoursEnd != "07:30" || got.QuietHoursTZ != "Asia/Shanghai" {
		t.Fatalf("unexpected quiet hours: enabled=%v, start=%s, end=%s, tz=%s",
			got.QuietHoursEnabled, got.QuietHoursStart, got.QuietHoursEnd, got.QuietHoursTZ)
	}
}

func TestNotificationChannelAllPeriodicReportsCanBeDisabled(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	// 父开关开启、三个子开关全部关闭是合法状态：更新路径必须原样保存，不能回填。
	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                   ulid.Make().String(),
		ChannelType:          store.ChannelTelegram,
		Name:                 "tg-silent",
		Enabled:              true,
		Target:               "12345",
		DigestEnabled:        true,
		ReceiveDailyDigest:   true, // 先建一条正常记录
		ReceiveWeeklyReport:  true,
		ReceiveMonthlyReport: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 更新为全部关闭（DigestEnabled 仍为 true）
	ch.DigestEnabled = true
	ch.ReceiveDailyDigest = false
	ch.ReceiveWeeklyReport = false
	ch.ReceiveMonthlyReport = false
	saved, err := data.Channels().Upsert(ctx, ch)
	if err != nil {
		t.Fatalf("update all-off: %v", err)
	}
	if saved.ReceiveDailyDigest || saved.ReceiveWeeklyReport || saved.ReceiveMonthlyReport {
		t.Fatalf("全部关闭被静默改写: daily=%v weekly=%v monthly=%v",
			saved.ReceiveDailyDigest, saved.ReceiveWeeklyReport, saved.ReceiveMonthlyReport)
	}

	got, err := data.Channels().Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got.ReceiveDailyDigest || got.ReceiveWeeklyReport || got.ReceiveMonthlyReport {
		t.Fatalf("落库后被静默改写: daily=%v weekly=%v monthly=%v",
			got.ReceiveDailyDigest, got.ReceiveWeeklyReport, got.ReceiveMonthlyReport)
	}
}

func TestNotificationChannelStorePersistsGivenFlagsOnCreate(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	// 新建且未显式设置子开关：store 原样落库（零值 false），不做隐式回填；
	// 默认值由上层 HTTP 处理层补齐。否则「显式全关」与「未设置」无法区分。
	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:            ulid.Make().String(),
		ChannelType:   store.ChannelTelegram,
		Name:          "tg-legacy",
		Enabled:       true,
		Target:        "12345",
		DigestEnabled: true,
	})
	if err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if ch.ReceiveDailyDigest || ch.ReceiveWeeklyReport || ch.ReceiveMonthlyReport {
		t.Fatalf("store 不应隐式回填子开关: %+v", ch)
	}

	// 显式三个子开关全关则原样落库。
	off, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                   ulid.Make().String(),
		ChannelType:          store.ChannelTelegram,
		Name:                 "tg-off",
		Enabled:              true,
		Target:               "12346",
		DigestEnabled:        true,
		ReceiveDailyDigest:   false,
		ReceiveWeeklyReport:  false,
		ReceiveMonthlyReport: false,
	})
	if err != nil {
		t.Fatalf("create all-off: %v", err)
	}
	if off.ReceiveDailyDigest || off.ReceiveWeeklyReport || off.ReceiveMonthlyReport {
		t.Fatalf("新建显式全关被改写: %+v", off)
	}
}
