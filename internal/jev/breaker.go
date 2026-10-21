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

type circuitBreaker struct {
	mu           sync.Mutex
	state        breakerState
	failures     int
	cooldown     time.Duration
	openedAt     time.Time
	probing      bool
	now          func() time.Time
}

func newBreaker(now func() time.Time, cooldown time.Duration) *circuitBreaker {
	if now == nil {
		now = time.Now
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &circuitBreaker{
		now:      now,
		cooldown: cooldown,
		state:    stateClosed,
	}
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	currentTime := b.now()

	switch b.state {
	case stateClosed:
		return true
	case stateOpen:
		if currentTime.Sub(b.openedAt) >= b.cooldown {
			b.state = stateHalfOpen
			b.probing = true
			return true
		}
		return false
	case stateHalfOpen:
		if !b.probing {
			b.probing = true
			return true
		}
		return false
	default:
		return false
	}
}

func (b *circuitBreaker) record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if success {
		b.failures = 0
		b.probing = false
		b.state = stateClosed
		return
	}

	b.probing = false
	b.failures++
	if b.state == stateHalfOpen || b.failures >= 5 {
		b.state = stateOpen
		b.openedAt = b.now()
	}
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
		return "half-open"
	default:
		return "unknown"
	}
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
	// 尝试 HTTP-Date
	if t, err := http.ParseTime(value); err == nil {
		diff := t.Sub(now)
		if diff <= 0 {
			return 0
		}
		return diff
	}
	return 0
}
