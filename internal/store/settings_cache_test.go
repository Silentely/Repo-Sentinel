package store

import (
	"testing"
	"time"
)

func TestSettingsCacheHitAndMiss(t *testing.T) {
	c := newSettingsCache(time.Minute)
	row := SystemSetting{Key: "k", ValueJSON: []byte(`"v"`)}
	if _, ok := c.Get("k"); ok {
		t.Fatal("未写入的键不应命中")
	}
	c.Set("k", row)
	got, ok := c.Get("k")
	if !ok || string(got.ValueJSON) != `"v"` {
		t.Fatalf("写入后应命中且值一致: ok=%v got=%s", ok, got.ValueJSON)
	}
}

func TestSettingsCacheTTLExpiry(t *testing.T) {
	c := newSettingsCache(20 * time.Millisecond)
	c.Set("k", SystemSetting{Key: "k"})
	if _, ok := c.Get("k"); !ok {
		t.Fatal("TTL 内应命中")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("TTL 过期后不应命中")
	}
}

func TestSettingsCacheInvalidate(t *testing.T) {
	c := newSettingsCache(time.Minute)
	c.Set("k", SystemSetting{Key: "k", ValueJSON: []byte(`"old"`)})
	c.Invalidate("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("Invalidate 后不应命中")
	}
	// 失效后重新写入可取到新值（模拟 Upsert 后重新查询）。
	c.Set("k", SystemSetting{Key: "k", ValueJSON: []byte(`"new"`)})
	got, ok := c.Get("k")
	if !ok || string(got.ValueJSON) != `"new"` {
		t.Fatalf("失效后可重新写入: ok=%v got=%s", ok, got.ValueJSON)
	}
}

func TestSettingsCacheNilSafe(t *testing.T) {
	var c *settingsCache
	if _, ok := c.Get("k"); ok {
		t.Fatal("nil 缓存不应命中")
	}
	c.Set("k", SystemSetting{Key: "k"}) // 不应 panic
	c.Invalidate("k")                   // 不应 panic
}
