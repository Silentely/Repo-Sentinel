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

func TestBreakerPermitAtomicAcquireAndRelease(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    2,
		Cooldown:            10 * time.Second,
		HalfOpenConcurrency: 1,
	})

	// 触发熔断进入 Open
	p1 := b.AcquirePermit()
	b.RecordPermitResult(p1, false)
	p2 := b.AcquirePermit()
	b.RecordPermitResult(p2, false)

	if b.getState() != "open" {
		t.Fatalf("expected open, got %s", b.getState())
	}

	// 冷却时间到，并发 10 个 AcquirePermit，断言只放行 1 个探测请求
	now = now.Add(10 * time.Second)
	var allowedCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	var probePermit BreakerPermit

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := b.AcquirePermit()
			if p.Allowed {
				mu.Lock()
				allowedCount++
				probePermit = p
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowedCount != 1 {
		t.Fatalf("expected exactly 1 permit allowed, got %d", allowedCount)
	}
	if !probePermit.IsProbe {
		t.Fatal("allowed permit must be marked as probe")
	}

	// 释放名额（例如排队超时），名额应回退
	b.ReleasePermit(probePermit)
	// 再次尝试，应该可以再次获取探测凭证
	pRetry := b.AcquirePermit()
	if !pRetry.Allowed || !pRetry.IsProbe {
		t.Fatal("expected retry permit to be allowed after release")
	}
}

func TestBreakerOldInFlightRequestCannotCloseOpenBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    2,
		Cooldown:            20 * time.Second,
		HalfOpenConcurrency: 1,
	})

	// 请求 1 在 generation 1 获取凭证并发显（迟到）
	slowPermit := b.AcquirePermit()
	if !slowPermit.Allowed || slowPermit.Generation != 1 {
		t.Fatalf("unexpected slowPermit: %+v", slowPermit)
	}

	// 其它请求连续失败，触发熔断，generation 跃迁
	pFail1 := b.AcquirePermit()
	b.RecordPermitResult(pFail1, false)
	pFail2 := b.AcquirePermit()
	b.RecordPermitResult(pFail2, false)

	if b.getState() != "open" {
		t.Fatalf("expected open state, got %s", b.getState())
	}

	// 旧在途请求 slowPermit 迟到成功
	b.RecordPermitResult(slowPermit, true)

	// 断言：熔断器必须依然处于 open 状态，绝不能被旧请求唤醒为 closed
	if b.getState() != "open" {
		t.Fatalf("old in-flight request closed open breaker! state: %s", b.getState())
	}
}

func TestBreakerOldInFlightRequestCannotAffectHalfOpenProbe(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold:    2,
		Cooldown:            20 * time.Second,
		HalfOpenConcurrency: 1,
	})

	// 1. 旧请求在 generation 1 获取凭证并挂起
	oldPermit := b.AcquirePermit()
	if !oldPermit.Allowed || oldPermit.Generation != 1 {
		t.Fatalf("unexpected oldPermit: %+v", oldPermit)
	}

	// 2. 连续 2 次失败触发熔断进入 open 状态
	pFail1 := b.AcquirePermit()
	b.RecordPermitResult(pFail1, false)
	pFail2 := b.AcquirePermit()
	b.RecordPermitResult(pFail2, false)

	if b.getState() != "open" {
		t.Fatalf("expected open state, got %s", b.getState())
	}
	initialFailures := b.failures
	if initialFailures != 2 {
		t.Fatalf("expected failures=2, got %d", initialFailures)
	}

	// 3. 冷却时间过去，放行半开探测
	now = now.Add(20 * time.Second)
	probePermit := b.AcquirePermit()
	if !probePermit.Allowed || !probePermit.IsProbe {
		t.Fatalf("expected probe permit allowed, got %+v", probePermit)
	}
	if b.getState() != "half_open" {
		t.Fatalf("expected half_open state, got %s", b.getState())
	}

	// 4. 关键验证：探测挂起中，代次较旧的请求迟到返回成功
	b.RecordPermitResult(oldPermit, true)

	// 断言：不得影响当前探测，不得转为 closed，断路器依然处于 half_open，且 failures 保持不变
	if b.getState() != "half_open" {
		t.Fatalf("old in-flight success corrupted half_open probe! state: %s", b.getState())
	}
	if b.failures != initialFailures {
		t.Fatalf("failures count modified by old request: expected %d, got %d", initialFailures, b.failures)
	}
	if b.halfOpenInFlight != 1 {
		t.Fatalf("halfOpenInFlight corrupted: expected 1, got %d", b.halfOpenInFlight)
	}

	// 5. 关键验证：代次较旧的请求迟到返回失败
	b.RecordPermitResult(oldPermit, false)
	if b.getState() != "half_open" {
		t.Fatalf("old in-flight failure corrupted half_open probe! state: %s", b.getState())
	}
	if b.failures != initialFailures {
		t.Fatalf("failures count modified by old failure: expected %d, got %d", initialFailures, b.failures)
	}

	// 6. 此时真正的探测凭证完成并报告成功，断路器才恢复 Closed
	b.RecordPermitResult(probePermit, true)
	if b.getState() != "closed" {
		t.Fatalf("expected closed after probe success, got %s", b.getState())
	}
	if b.failures != 0 {
		t.Fatalf("expected failures reset to 0 after probe success, got %d", b.failures)
	}
}

func TestBreakerTripOpen(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(func() time.Time { return now }, BreakerConfig{
		FailureThreshold: 5,
		Cooldown:         30 * time.Second,
	})

	if b.getState() != "closed" {
		t.Fatal("expected initially closed")
	}

	// 显式 TripOpen（例如 429 Retry-After=60s）
	b.TripOpen(60 * time.Second)
	if b.getState() != "open" {
		t.Fatalf("expected open after TripOpen, got %s", b.getState())
	}

	// 30s 冷却后仍为 open（因为设定了 60s 冷却）
	now = now.Add(30 * time.Second)
	p := b.AcquirePermit()
	if p.Allowed {
		t.Fatal("should not allow at 30s when TripOpen cooldown is 60s")
	}

	// 60s 到期后允许探测
	now = now.Add(31 * time.Second)
	p2 := b.AcquirePermit()
	if !p2.Allowed || !p2.IsProbe {
		t.Fatal("should allow probe after 60s cooldown")
	}
}
