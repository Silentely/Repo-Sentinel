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
