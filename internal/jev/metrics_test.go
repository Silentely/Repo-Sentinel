package jev

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestMetricsCollector_PersistenceAndRecovery(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	st, err := store.Open(ctx, config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + dbPath,
	})
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer st.Close()

	collector := newMetricsCollector()
	if err := collector.BindStore(ctx, st); err != nil {
		t.Fatalf("bind store failed: %v", err)
	}

	// 记录各种指标
	collector.RecordCall("notification")
	collector.RecordCall("notification")
	collector.RecordFailure("notification")
	collector.RecordCacheHit("notification")
	collector.RecordFallback("notification")
	collector.RecordSuppressed()
	collector.RecordWouldSuppress()
	collector.RecordSkipped()
	collector.RecordWouldSkip()

	// 同步保存确保写入
	if err := collector.SaveToStore(ctx); err != nil {
		t.Fatalf("save to store failed: %v", err)
	}

	// 模拟重启：创建全新的 collector 并重新加载
	newCollector := newMetricsCollector()
	if err := newCollector.LoadFromStore(ctx, st); err != nil {
		t.Fatalf("load from store failed: %v", err)
	}

	snap := newCollector.Snapshot()
	if snap.Calls["notification"] != 2 {
		t.Errorf("expected 2 calls, got %d", snap.Calls["notification"])
	}
	if snap.Failures["notification"] != 1 {
		t.Errorf("expected 1 failure, got %d", snap.Failures["notification"])
	}
	if snap.CacheHits["notification"] != 1 {
		t.Errorf("expected 1 cache hit, got %d", snap.CacheHits["notification"])
	}
	if snap.Fallbacks["notification"] != 1 {
		t.Errorf("expected 1 fallback, got %d", snap.Fallbacks["notification"])
	}
	if snap.Suppressed != 1 {
		t.Errorf("expected 1 suppressed, got %d", snap.Suppressed)
	}
	if snap.WouldSuppress != 1 {
		t.Errorf("expected 1 would_suppress, got %d", snap.WouldSuppress)
	}
	if snap.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", snap.Skipped)
	}
	if snap.WouldSkip != 1 {
		t.Errorf("expected 1 would_skip, got %d", snap.WouldSkip)
	}

	// 测试异步保存机制
	collector.RecordCall("pr_gate")
	// 等待一小会儿确保异步持久化完成
	time.Sleep(150 * time.Millisecond)

	asyncCheckCollector := newMetricsCollector()
	if err := asyncCheckCollector.LoadFromStore(ctx, st); err != nil {
		t.Fatalf("load async saved store failed: %v", err)
	}
	if asyncCheckCollector.Snapshot().Calls["pr_gate"] != 1 {
		t.Errorf("expected 1 pr_gate call after async persist, got %d", asyncCheckCollector.Snapshot().Calls["pr_gate"])
	}
}
