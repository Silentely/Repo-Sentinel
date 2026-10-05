package githubx

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

// parseRateLimitError 统一解析 GitHub 出站 HTTP 响应中的 429 与 403 限流。
// 若不属于限流响应，返回 (false, nil)。
func parseRateLimitError(resp *http.Response, body []byte) (bool, *RateLimitError) {
	if resp == nil {
		return false, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		rle := &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
		GlobalCoordinator().RecordRateLimit(rle.RetryAfter)
		return true, rle
	}
	if resp.StatusCode == http.StatusForbidden {
		bodyLower := bytes.ToLower(body)
		// 1. 次级限流/滥用检测 (Secondary Rate Limit)
		if bytes.Contains(bodyLower, []byte("secondary rate limit")) || bytes.Contains(bodyLower, []byte("abuse detection")) {
			retryAfter := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
			if retryAfter <= 0 {
				retryAfter = 60 * time.Second // 默认保守冷却 60s
			}
			rle := &RateLimitError{RetryAfter: retryAfter}
			GlobalCoordinator().RecordRateLimit(rle.RetryAfter)
			return true, rle
		}
		// 2. 主限流 (Primary Rate Limit)
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || bytes.Contains(bodyLower, []byte("rate limit")) {
			rle := &RateLimitError{RetryAfter: resetDelta(resp.Header.Get("X-RateLimit-Reset"))}
			GlobalCoordinator().RecordRateLimit(rle.RetryAfter)
			return true, rle
		}
	}
	return false, nil
}


// RateLimitCoordinator 统一协调 REST 与 GraphQL 的限流退避状态。
type RateLimitCoordinator struct {
	mu            sync.RWMutex
	cooldownUntil time.Time
}

var globalCoordinator = &RateLimitCoordinator{}

// GlobalCoordinator 返回全局限流协调器单例。
func GlobalCoordinator() *RateLimitCoordinator {
	return globalCoordinator
}

// RecordRateLimit 记录限流事件并更新冷却截止时间。
func (c *RateLimitCoordinator) RecordRateLimit(d time.Duration) {
	if d <= 0 {
		d = 60 * time.Second
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	target := time.Now().Add(d)
	if target.After(c.cooldownUntil) {
		c.cooldownUntil = target
	}
}

// IsCoolingDown 检查当前是否处于全局限流冷却期中。
func (c *RateLimitCoordinator) IsCoolingDown() (bool, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	remaining := time.Until(c.cooldownUntil)
	if remaining > 0 {
		return true, remaining
	}
	return false, 0
}

// Reset 重置冷却时间（用于测试）。
func (c *RateLimitCoordinator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cooldownUntil = time.Time{}
}
