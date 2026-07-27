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
)

type loginLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// LoginLimiter 按 IP 与规范化用户名隔离登录令牌桶。
type LoginLimiter struct {
	mu        sync.Mutex
	clock     Clock
	entries   map[string]*loginLimiterEntry
	lastPrune time.Time
}

// NewLoginLimiter 创建每分钟五次、突发五次的并发安全登录限流器。
func NewLoginLimiter(clock Clock) *LoginLimiter {
	if clock == nil {
		clock = systemClock{}
	}
	now := clock.Now().UTC()
	return &LoginLimiter{
		clock:     clock,
		entries:   make(map[string]*loginLimiterEntry),
		lastPrune: now,
	}
}

// Allow 消耗指定 IP 与用户名键的一次登录尝试额度。
func (l *LoginLimiter) Allow(ip, normalizedUsername string) bool {
	now := l.clock.Now().UTC()
	key := strings.TrimSpace(ip) + "\x00" + strings.ToLower(strings.TrimSpace(normalizedUsername))

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

func (l *LoginLimiter) prune(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) >= loginLimiterEntryTTL {
			delete(l.entries, key)
		}
	}
	l.lastPrune = now
}
