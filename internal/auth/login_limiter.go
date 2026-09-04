package auth

import (
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	loginLimiterBurst         = 5
	loginLimiterRefill        = 12 * time.Second
	loginLimiterPruneInterval = time.Minute
	loginLimiterEntryTTL      = 10 * time.Minute
	accountFailureTTL         = 15 * time.Minute
)

type loginLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type accountFailureEntry struct {
	consecutiveFailures int
	lastFailed          time.Time
}

// LoginLimiter 按来源 IP 隔离登录令牌桶，并维护账号连续失败渐进延迟。
type LoginLimiter struct {
	mu              sync.Mutex
	clock           Clock
	entries         map[string]*loginLimiterEntry
	accountFailures map[string]*accountFailureEntry
	lastPrune       time.Time
}

// NewLoginLimiter 创建每分钟五次、突发五次的并发安全登录限流器。
func NewLoginLimiter(clock Clock) *LoginLimiter {
	if clock == nil {
		clock = systemClock{}
	}
	now := clock.Now().UTC()
	return &LoginLimiter{
		clock:           clock,
		entries:         make(map[string]*loginLimiterEntry),
		accountFailures: make(map[string]*accountFailureEntry),
		lastPrune:       now,
	}
}

// Allow 消耗指定来源 IP 的一次登录尝试额度。
func (l *LoginLimiter) Allow(ip string) bool {
	now := l.clock.Now().UTC()
	key := strings.TrimSpace(ip)

	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastPrune) >= loginLimiterPruneInterval {
		l.prune(now)
	}
	entry := l.entries[key]
	if entry == nil {
		entry = &loginLimiterEntry{
			limiter: rate.NewLimiter(rate.Every(loginLimiterRefill), loginLimiterBurst),
		}
		l.entries[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter.AllowN(now, 1)
}

// RecordFailure 记录指定账号的一次认证失败。
func (l *LoginLimiter) RecordFailure(account string) {
	key := strings.TrimSpace(strings.ToLower(account))
	if key == "" {
		return
	}
	now := l.clock.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.accountFailures[key]
	if entry == nil || now.Sub(entry.lastFailed) > accountFailureTTL {
		entry = &accountFailureEntry{}
		l.accountFailures[key] = entry
	}
	entry.consecutiveFailures++
	entry.lastFailed = now
}

// RecordSuccess 在账号认证成功后清零连续失败计数。
func (l *LoginLimiter) RecordSuccess(account string) {
	key := strings.TrimSpace(strings.ToLower(account))
	if key == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.accountFailures, key)
}

// DelayFor 根据账号连续失败次数计算渐进式延迟惩罚。
func (l *LoginLimiter) DelayFor(account string) time.Duration {
	key := strings.TrimSpace(strings.ToLower(account))
	if key == "" {
		return 0
	}
	now := l.clock.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.accountFailures[key]
	if entry == nil {
		return 0
	}
	if now.Sub(entry.lastFailed) > accountFailureTTL {
		delete(l.accountFailures, key)
		return 0
	}
	switch {
	case entry.consecutiveFailures >= 5:
		return 2 * time.Second
	case entry.consecutiveFailures == 4:
		return 1 * time.Second
	case entry.consecutiveFailures == 3:
		return 500 * time.Millisecond
	default:
		return 0
	}
}

func (l *LoginLimiter) prune(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) >= loginLimiterEntryTTL {
			delete(l.entries, key)
		}
	}
	for key, entry := range l.accountFailures {
		if now.Sub(entry.lastFailed) >= accountFailureTTL {
			delete(l.accountFailures, key)
		}
	}
	l.lastPrune = now
}
