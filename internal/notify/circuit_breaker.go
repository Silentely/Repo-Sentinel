package notify

import (
	"sync"
	"time"
)

// CircuitState 熔断器状态
type CircuitState string

const (
	CircuitClosed   CircuitState = "CLOSED"    // 正常，放行请求
	CircuitOpen     CircuitState = "OPEN"      // 熔断中，直接拒绝
	CircuitHalfOpen CircuitState = "HALF_OPEN" // 试探恢复中
)

// ChannelCircuitBreaker 渠道专用熔断控制器
type ChannelCircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	cooldown         time.Duration
	lastFailureTime  time.Time
}

// NewChannelCircuitBreaker 创建熔断器
func NewChannelCircuitBreaker(failureThreshold, successThreshold int, cooldown time.Duration) *ChannelCircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if successThreshold <= 0 {
		successThreshold = 2
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &ChannelCircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		cooldown:         cooldown,
	}
}

// Allow 判断当前是否允许发起请求
func (cb *ChannelCircuitBreaker) Allow(now time.Time) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitClosed {
		return true
	}

	if cb.state == CircuitOpen {
		if now.Sub(cb.lastFailureTime) >= cb.cooldown {
			// 冷却期已过，进入半开试探状态
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			return true
		}
		return false
	}

	// 半开状态放行试探流量
	return true
}

// RecordSuccess 记录一次成功
func (cb *ChannelCircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			// 试探成功，彻底闭合熔断器恢复正常
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	} else if cb.state == CircuitClosed {
		cb.failureCount = 0
	}
}

// RecordFailure 记录一次失败
func (cb *ChannelCircuitBreaker) RecordFailure(now time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = now

	if cb.state == CircuitHalfOpen {
		// 试探失败，立即重新进入熔断
		cb.state = CircuitOpen
	} else if cb.state == CircuitClosed {
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.state = CircuitOpen
		}
	}
}

// State 获取当前熔断状态
func (cb *ChannelCircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
