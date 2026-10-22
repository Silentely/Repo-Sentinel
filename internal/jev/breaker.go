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

// BreakerPermit 是一次允许调用的探测凭证，与当前熔断器的代次绑定。
type BreakerPermit struct {
	Allowed    bool
	Generation uint64
	IsProbe    bool
}

type circuitBreaker struct {
	mu               sync.Mutex
	state            breakerState
	generation       uint64
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
		generation:  1,
		now:         now,
		threshold:   threshold,
		cooldown:    cooldown,
		halfOpenMax: halfOpenMax,
		stats: BreakerStats{
			State: "closed",
		},
	}
}

// AcquirePermit 在同一临界区内原子判定是否放行、推进状态、递增半开计数并返回凭证。
func (b *circuitBreaker) AcquirePermit() BreakerPermit {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	switch b.state {
	case stateClosed:
		return BreakerPermit{
			Allowed:    true,
			Generation: b.generation,
			IsProbe:    false,
		}
	case stateOpen:
		if now.Sub(b.openedAt) >= b.cooldown {
			b.transitionLocked(stateHalfOpen)
			b.halfOpenInFlight = 1
			return BreakerPermit{
				Allowed:    true,
				Generation: b.generation,
				IsProbe:    true,
			}
		}
		return BreakerPermit{Allowed: false, Generation: b.generation}
	case stateHalfOpen:
		if b.halfOpenInFlight < b.halfOpenMax {
			b.halfOpenInFlight++
			return BreakerPermit{
				Allowed:    true,
				Generation: b.generation,
				IsProbe:    true,
			}
		}
		return BreakerPermit{Allowed: false, Generation: b.generation}
	default:
		return BreakerPermit{Allowed: false, Generation: b.generation}
	}
}

// ReleasePermit 在请求提前取消、排队超时或非上游失败时释放半开名额。
func (b *circuitBreaker) ReleasePermit(permit BreakerPermit) {
	if !permit.Allowed || !permit.IsProbe {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if permit.Generation != b.generation || b.state != stateHalfOpen {
		return
	}
	if b.halfOpenInFlight > 0 {
		b.halfOpenInFlight--
	}
}

// RecordPermitResult 报告与凭证绑定的执行结果。
func (b *circuitBreaker) RecordPermitResult(permit BreakerPermit, success bool) {
	if !permit.Allowed {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	// 规则 3：若代次不匹配，说明该请求属于旧代次，绝不作为探测成功关闭熔断
	if permit.Generation != b.generation {
		// 代次已变（例如已经在某次失败后转为了新 Open），旧请求即便成功也不能恢复熔断
		return
	}

	if success {
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
			// open 分支绝不清理 failures 计数，保持当前状态
		}
	} else {
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
}

// 保持向下兼容的 allow 与 record 方法
func (b *circuitBreaker) allow() bool {
	p := b.AcquirePermit()
	return p.Allowed
}

func (b *circuitBreaker) record(success bool) {
	b.mu.Lock()
	gen := b.generation
	st := b.state
	b.mu.Unlock()
	b.RecordPermitResult(BreakerPermit{
		Allowed:    true,
		Generation: gen,
		IsProbe:    st == stateHalfOpen,
	}, success)
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
	b.generation++
}

func (b *circuitBreaker) getGeneration() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.generation
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

// TripOpen 显式将断路器转入 Open 状态并设定冷却时间（如与长时间 Retry-After 联动）。
func (b *circuitBreaker) TripOpen(cooldown time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cooldown > 0 {
		b.cooldown = cooldown
	}
	b.transitionLocked(stateOpen)
	b.openedAt = b.now()
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
