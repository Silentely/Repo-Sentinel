package notify

import (
	"testing"
	"time"
)

func TestSmoothTokenBucket_BurstAndThrottle(t *testing.T) {
	now := time.Now()
	// 容量 3 个，每秒补充 1 个
	tb := NewSmoothTokenBucket(3, 1)

	// 连续消费 3 个令牌应该全部成功
	for i := 0; i < 3; i++ {
		allowed, _ := tb.AllowN(now, 1)
		if !allowed {
			t.Fatalf("expected token %d to be allowed in burst", i+1)
		}
	}

	// 第 4 次消费应该被拦截
	allowed, retryAfter := tb.AllowN(now, 1)
	if allowed {
		t.Fatalf("expected 4th request to be throttled")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter duration, got %v", retryAfter)
	}

	// 时间前进 1 秒后，应已补充 1 个令牌
	allowedAfterSec, _ := tb.AllowN(now.Add(1*time.Second), 1)
	if !allowedAfterSec {
		t.Fatalf("expected request after 1 second refill to be allowed")
	}
}
