package digest

import (
	"encoding/json"
	"path/filepath"
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
		Enabled: true, Target: "1",
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
