package notify

import (
	"testing"
	"time"
)

func TestDedupeCache(t *testing.T) {
	cache := NewDedupeCache()
	now := time.Now()
	ttl := 10 * time.Second

	// 第一次写入，不是重复
	dup1 := cache.CheckAndSet("msg-123", ttl, now)
	if dup1 {
		t.Fatalf("expected first write to not be duplicate")
	}

	// 5 秒后再次写入同一 key，必须判定为重复
	dup2 := cache.CheckAndSet("msg-123", ttl, now.Add(5*time.Second))
	if !dup2 {
		t.Fatalf("expected duplicate hit within ttl")
	}
	if cache.DuplicateCount() != 1 {
		t.Errorf("expected 1 duplicate hit, got %d", cache.DuplicateCount)
	}

	// 15 秒后过期，再次写入，不再判定为重复
	dup3 := cache.CheckAndSet("msg-123", ttl, now.Add(15*time.Second))
	if dup3 {
		t.Fatalf("expected not duplicate after ttl expiry")
	}
}

func TestDedupeCache_PruneExpired(t *testing.T) {
	cache := NewDedupeCache()
	now := time.Now()

	cache.CheckAndSet("a", 5*time.Second, now)
	cache.CheckAndSet("b", 10*time.Second, now)

	// 6 秒后清理，应该清理掉 "a"
	pruned := cache.PruneExpired(now.Add(6 * time.Second))
	if pruned != 1 {
		t.Errorf("expected 1 pruned item, got %d", pruned)
	}
}
