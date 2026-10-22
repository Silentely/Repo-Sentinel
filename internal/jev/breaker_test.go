package jev

import (
	"sync"
	"testing"
	"time"
)

func TestBreakerHalfOpen(t *testing.T) {
	now := time.Unix(0, 0)
	breaker := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    5,
		Cooldown:            30 * time.Second,
		HalfOpenConcurrency: 1,
	})
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

	stats := breaker.getStats()
	if stats.ClosedToOpen != 1 {
		t.Errorf("expected 1 closed_to_open, got %d", stats.ClosedToOpen)
	}
	if stats.OpenToHalfOpen != 1 {
		t.Errorf("expected 1 open_to_half_open, got %d", stats.OpenToHalfOpen)
	}
	if stats.HalfOpenToClosed != 1 {
		t.Errorf("expected 1 half_open_to_closed, got %d", stats.HalfOpenToClosed)
	}
	if stats.TotalTransitions != 3 {
		t.Errorf("expected 3 total transitions, got %d", stats.TotalTransitions)
	}
}

func TestBreakerConcurrentProbing(t *testing.T) {
	now := time.Unix(0, 0)
	breaker := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    5,
		Cooldown:            30 * time.Second,
		HalfOpenConcurrency: 1,
	})
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

	// 探测失败重回 Open 状态
	breaker.record(false)
	stats := breaker.getStats()
	if stats.State != "open" {
		t.Fatalf("探测失败应重回 open, got %s", stats.State)
	}
	if stats.HalfOpenToOpen != 1 {
		t.Fatalf("expected 1 half_open_to_open, got %d", stats.HalfOpenToOpen)
	}
}

func TestBreakerConfigurable(t *testing.T) {
	now := time.Unix(0, 0)
	// 测试自定义参数：阈值 3，冷却 10s，半开并发 2
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    3,
		Cooldown:            10 * time.Second,
		HalfOpenConcurrency: 2,
	})

	if b.getState() != "closed" {
		t.Fatalf("expected closed, got %s", b.getState())
	}

	b.record(false)
	b.record(false)
	if !b.allow() {
		t.Fatal("2 次失败不应熔断")
	}
	b.record(false) // 第 3 次失败触发熔断
	if b.allow() {
		t.Fatal("3 次失败应触发熔断")
	}
	if b.getState() != "open" {
		t.Fatalf("expected open, got %s", b.getState())
	}

	// 8 秒后仍未达 10s
	now = now.Add(8 * time.Second)
	if b.allow() {
		t.Fatal("8 秒后不应放行")
	}

	// 达到 10s 冷却，允许 2 个半开探测并发
	now = now.Add(2 * time.Second)
	if !b.allow() {
		t.Fatal("首个探测应允许")
	}
	if !b.allow() {
		t.Fatal("第 2 个并发探测应允许")
	}
	if b.allow() {
		t.Fatal("超过半开并发上限应快速失败")
	}
	if b.getState() != "half_open" {
		t.Fatalf("expected half_open, got %s", b.getState())
	}
}

func TestBreakerConcurrentReadWriteRace(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    5,
		Cooldown:            100 * time.Millisecond,
		HalfOpenConcurrency: 1,
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 读 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = b.allow()
					_ = b.getState()
					_ = b.getStats()
				}
			}
		}()
	}

	// 写 goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				b.record(j%2 == 0)
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// 1. 空字符串
	if d := parseRetryAfter("", now); d != 0 {
		t.Errorf("expected 0 for empty, got %v", d)
	}
	// 2. 秒数
	if d := parseRetryAfter("120", now); d != 120*time.Second {
		t.Errorf("expected 120s, got %v", d)
	}
	// 3. 负秒数
	if d := parseRetryAfter("-5", now); d != 0 {
		t.Errorf("expected 0 for negative, got %v", d)
	}
	// 4. HTTP-Date
	target := now.Add(45 * time.Second)
	httpDate := target.Format(time.RFC1123)
	if d := parseRetryAfter(httpDate, now); d != 45*time.Second {
		t.Errorf("expected 45s, got %v", d)
	}
	// 5. 过去时间
	pastDate := now.Add(-10 * time.Second).Format(time.RFC1123)
	if d := parseRetryAfter(pastDate, now); d != 0 {
		t.Errorf("expected 0 for past date, got %v", d)
	}
	// 6. 非法格式
	if d := parseRetryAfter("invalid-date", now); d != 0 {
		t.Errorf("expected 0 for invalid, got %v", d)
	}
}
