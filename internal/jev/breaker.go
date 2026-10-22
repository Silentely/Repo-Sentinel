package jev

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

type breakerState int

const (
	stateClosed breakerState = iota
	stateOpen
	stateHalfOpen
)

// BreakerConfig 控制断路器的运行时参数。
type BreakerConfig struct {
	FailureThreshold    int           // 连续失败次数触发熔断，默认 5
	Cooldown            time.Duration // 熔断冷却开窗时长，默认 30s
	HalfOpenConcurrency int           // 半开状态放行并发数，默认 1
}

// BreakerStats 记录断路器的运行状态与流转次数。
type BreakerStats struct {
	State            string `json:"state"`
	ClosedToOpen     int64  `json:"closed_to_open"`
	OpenToHalfOpen   int64  `json:"open_to_half_open"`
	HalfOpenToClosed int64  `json:"half_open_to_closed"`
	HalfOpenToOpen   int64  `json:"half_open_to_open"`
	TotalTransitions int64  `json:"total_transitions"`
}

type circuitBreaker struct {
	mu               sync.Mutex
	state            breakerState
	failures         int
	openedAt         time.Time
	cooldown         time.Duration
	threshold        int
	halfOpenMax      int
	halfOpenInFlight int
	now              func() time.Time
	stats            BreakerStats
}

func newBreaker(now func() time.Time, cfg BreakerConfig) *circuitBreaker {
	if now == nil {
		now = time.Now
	}
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	cooldown := cfg.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	halfOpenMax := cfg.HalfOpenConcurrency
	if halfOpenMax <= 0 {
		halfOpenMax = 1
	}
	return &circuitBreaker{
		state:       stateClosed,
		now:         now,
		threshold:   threshold,
		cooldown:    cooldown,
		halfOpenMax: halfOpenMax,
		stats: BreakerStats{
			State: "closed",
		},
	}
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	switch b.state {
	case stateClosed:
		return true
	case stateOpen:
		if now.Sub(b.openedAt) >= b.cooldown {
			b.transitionLocked(stateHalfOpen)
			b.halfOpenInFlight = 1
			return true
		}
		return false
	case stateHalfOpen:
		// 纪律：半开状态只放行指定并发数（默认1个请求），其余快速失败
		if b.halfOpenInFlight < b.halfOpenMax {
			b.halfOpenInFlight++
			return true
		}
		return false
	default:
		return false
	}
}

func (b *circuitBreaker) record(success bool) {
	if success {
		b.recordSuccess()
	} else {
		b.recordFailure()
	}
}

func (b *circuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		b.failures = 0
	case stateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		b.failures = 0
		b.transitionLocked(stateClosed)
	case stateOpen:
		// 理论上不会发生，但重置计数
		b.failures = 0
	}
}

func (b *circuitBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		b.failures++
		if b.failures >= b.threshold {
			b.transitionLocked(stateOpen)
			b.openedAt = b.now()
		}
	case stateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		b.transitionLocked(stateOpen)
		b.openedAt = b.now()
	case stateOpen:
		// 保持 open
	}
}

func (b *circuitBreaker) transitionLocked(next breakerState) {
	if b.state == next {
		return
	}
	switch b.state {
	case stateClosed:
		if next == stateOpen {
			b.stats.ClosedToOpen++
		}
	case stateOpen:
		if next == stateHalfOpen {
			b.stats.OpenToHalfOpen++
		}
	case stateHalfOpen:
		if next == stateClosed {
			b.stats.HalfOpenToClosed++
		} else if next == stateOpen {
			b.stats.HalfOpenToOpen++
		}
	}
	b.stats.TotalTransitions++
	b.state = next
}

func (b *circuitBreaker) getState() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateClosed:
		return "closed"
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

func (b *circuitBreaker) getStats() BreakerStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.stats
	switch b.state {
	case stateClosed:
		st.State = "closed"
	case stateOpen:
		st.State = "open"
	case stateHalfOpen:
		st.State = "half_open"
	default:
		st.State = "unknown"
	}
	return st
}

// parseRetryAfter 解析 Retry-After 标头（秒数或 HTTP-Date）。
func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	// 尝试秒数
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	// 尝试 HTTP-Date (GMT / RFC1123)
	if t, err := http.ParseTime(value); err == nil {
		diff := t.Sub(now)
		if diff <= 0 {
			return 0
		}
		return diff
	}
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		diff := t.Sub(now)
		if diff <= 0 {
			return 0
		}
		return diff
	}
	return 0
}
