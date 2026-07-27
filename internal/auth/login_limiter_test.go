package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Test登录限流允许五次突发并拒绝第六次(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC))
	limiter := NewLoginLimiter(clock)
	for attempt := 1; attempt <= 5; attempt++ {
		if !limiter.Allow("127.0.0.1") {
			t.Fatalf("第 %d 次登录尝试应被允许", attempt)
		}
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("同一 IP 的第六次尝试应立即拒绝")
	}

	clock.Advance(12 * time.Second)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("推进一个补充周期后应恢复一个令牌")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("刚消耗补充令牌后下一次尝试仍应拒绝")
	}
}

func Test登录限流按IP隔离且不同IP使用独立桶(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 27, 18, 30, 0, 0, time.UTC))
	limiter := NewLoginLimiter(clock)
	for attempt := 0; attempt < 5; attempt++ {
		if !limiter.Allow("127.0.0.1") {
			t.Fatalf("准备主桶第 %d 次尝试失败", attempt+1)
		}
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("同一 IP 的第六次尝试应拒绝")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("同一 IP 的限流桶数量=%d，期望 1", len(limiter.entries))
	}
	if !limiter.Allow("127.0.0.2") {
		t.Fatal("不同 RemoteAddr 不应共享登录限流桶")
	}
}

func Test登录限流机会式清理陈旧桶(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 27, 19, 0, 0, 0, time.UTC))
	limiter := NewLoginLimiter(clock)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("创建陈旧桶的首次尝试应允许")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("限流桶数量=%d，期望 1", len(limiter.entries))
	}

	clock.Advance(11 * time.Minute)
	if !limiter.Allow("127.0.0.2") {
		t.Fatal("新桶首次尝试应允许")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("机会式清理后限流桶数量=%d，期望仅保留新桶", len(limiter.entries))
	}
}

func Test登录限流并发同一键最多允许五次(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 27, 19, 30, 0, 0, time.UTC))
	limiter := NewLoginLimiter(clock)
	var allowed atomic.Int32
	var group sync.WaitGroup
	for attempt := 0; attempt < 32; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if limiter.Allow("127.0.0.1") {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("并发同键允许次数=%d，期望恰好 5", allowed.Load())
	}
}
