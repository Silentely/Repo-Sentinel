package rules

import (
	"context"
	"path/filepath"
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
}

func TestHTMLEscape(t *testing.T) {
	got := htmlEscape(`a<b>&"c`)
	want := "a&lt;b&gt;&amp;&quot;c"
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
