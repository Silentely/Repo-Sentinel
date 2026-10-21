package jev

import (
	"sync"
	"testing"
	"time"
)

func TestBreakerHalfOpen(t *testing.T) {
	now := time.Unix(0, 0)
	breaker := newBreaker(func() time.Time { return now }, 30*time.Second)
	for i := 0; i < 5; i++ {
		if !breaker.allow() {
			t.Fatal("提前熔断")
		}
		breaker.record(false)
	}
	if breaker.allow() {
		t.Fatal("Open 不得放行")
	}
	now = now.Add(30 * time.Second)
	if !breaker.allow() || breaker.allow() {
		t.Fatal("半开只允许一个探测")
	}
	breaker.record(true)
	if !breaker.allow() {
		t.Fatal("成功后应恢复 Closed")
	}
}

func TestBreakerConcurrentProbing(t *testing.T) {
	now := time.Unix(0, 0)
	breaker := newBreaker(func() time.Time { return now }, 30*time.Second)
	for i := 0; i < 5; i++ {
		breaker.record(false)
	}
	now = now.Add(30 * time.Second)

	var allowedCount int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if breaker.allow() {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowedCount != 1 {
		t.Fatalf("半开状态下 100 并发抢占只能放行 1 个探测，实际放行 %d", allowedCount)
	}
}
