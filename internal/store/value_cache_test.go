package store

import (
	"testing"
	"time"
)

func TestTTLValueCacheHitMissAndInvalidate(t *testing.T) {
	c := newTTLValueCache[[]string](time.Minute)
	if _, ok := c.Get(); ok {
		t.Fatal("未写入时不应命中")
	}
	c.Set([]string{"a", "b"})
	got, ok := c.Get()
	if !ok || len(got) != 2 || got[0] != "a" {
		t.Fatalf("写入后应命中且值一致: ok=%v got=%v", ok, got)
	}
	c.Invalidate()
	if _, ok := c.Get(); ok {
		t.Fatal("Invalidate 后不应命中")
	}
	// 失效后可重新写入（模拟写路径失效后重新查询回填）。
	c.Set([]string{"c"})
	got, ok = c.Get()
	if !ok || len(got) != 1 || got[0] != "c" {
		t.Fatalf("失效后可重新写入: ok=%v got=%v", ok, got)
	}
}

func TestTTLValueCacheExpiry(t *testing.T) {
	c := newTTLValueCache[int](20 * time.Millisecond)
	c.Set(42)
	if _, ok := c.Get(); !ok {
		t.Fatal("TTL 内应命中")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get(); ok {
		t.Fatal("TTL 过期后不应命中")
	}
}

func TestTTLValueCacheNilSafe(t *testing.T) {
	var c *ttlValueCache[int]
	if _, ok := c.Get(); ok {
		t.Fatal("nil 缓存不应命中")
	}
	c.Set(1)       // 不应 panic
	c.Invalidate() // 不应 panic
}
