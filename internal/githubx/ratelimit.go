package githubx

import (
	"bytes"
	"net/http"
	"time"
)

// parseRateLimitError 统一解析 GitHub 出站 HTTP 响应中的 429 与 403 限流。
// 若不属于限流响应，返回 (false, nil)。
func parseRateLimitError(resp *http.Response, body []byte) (bool, *RateLimitError) {
	if resp == nil {
		return false, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true, &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode == http.StatusForbidden {
		bodyLower := bytes.ToLower(body)
		// 1. 次级限流/滥用检测 (Secondary Rate Limit)
		if bytes.Contains(bodyLower, []byte("secondary rate limit")) || bytes.Contains(bodyLower, []byte("abuse detection")) {
			retryAfter := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
			if retryAfter <= 0 {
				retryAfter = 60 * time.Second // 默认保守冷却 60s
			}
			return true, &RateLimitError{RetryAfter: retryAfter}
		}
		// 2. 主限流 (Primary Rate Limit)
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || bytes.Contains(bodyLower, []byte("rate limit")) {
			return true, &RateLimitError{RetryAfter: resetDelta(resp.Header.Get("X-RateLimit-Reset"))}
		}
	}
	return false, nil
}
