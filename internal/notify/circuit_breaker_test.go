package notify

import (
	"testing"
	"time"
)

func TestChannelCircuitBreaker(t *testing.T) {
	now := time.Now()
	// 阈值：失败 2 次进入熔断，冷却 10 秒，半开成功 1 次恢复
	cb := NewChannelCircuitBreaker(2, 1, 10*time.Second)

	if !cb.Allow(now) || cb.State() != CircuitClosed {
		t.Fatalf("expected initial state CLOSED")
	}

	// 第一次失败
	cb.RecordFailure(now)
	if !cb.Allow(now) || cb.State() != CircuitClosed {
		t.Fatalf("expected still CLOSED after 1 failure")
	}

	// 第二次失败，触发熔断
	cb.RecordFailure(now)
	if cb.State() != CircuitOpen {
		t.Fatalf("expected OPEN after 2 failures")
	}
	if cb.Allow(now) {
		t.Fatalf("expected request rejected while OPEN")
	}

	// 冷却期过后 (11秒后)，尝试请求进入 HALF_OPEN
	allowProbe := cb.Allow(now.Add(11 * time.Second))
	if !allowProbe || cb.State() != CircuitHalfOpen {
		t.Fatalf("expected HALF_OPEN and allowed after cooldown")
	}

	// 试探成功，恢复 CLOSED
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected CLOSED after probe success")
	}
}
