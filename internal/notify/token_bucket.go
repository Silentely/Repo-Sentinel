package notify

import (
	"sync"
	"time"
)

// SmoothTokenBucket 内存平滑令牌桶流量整形器
type SmoothTokenBucket struct {
	mu         sync.Mutex
	capacity   float64   // 桶最大容量
	tokens     float64   // 当前剩余令牌数
	refillRate float64   // 每秒生成的令牌数
	lastRefill time.Time // 上次补充时间
}

// NewSmoothTokenBucket 创建令牌桶实例
// capacity: 允许的最大突发脉冲量
// refillRate: 每秒持续补充速率 (如 20.0 表示 20 tokens/sec)
func NewSmoothTokenBucket(capacity, refillRate float64) *SmoothTokenBucket {
	if capacity <= 0 {
		capacity = 1
	}
	if refillRate <= 0 {
		refillRate = 1
	}
	return &SmoothTokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow 尝试消费 1 个令牌。若桶内有令牌则立即放行，否则拒绝并返回需等待的时长
func (tb *SmoothTokenBucket) Allow() (allowed bool, retryAfter time.Duration) {
	return tb.AllowN(time.Now(), 1.0)
}

// AllowN 允许注入时间戳进行无时钟漂移的确定性消费判断
func (tb *SmoothTokenBucket) AllowN(now time.Time, requested float64) (allowed bool, retryAfter time.Duration) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// 补充令牌
	elapsed := now.Sub(tb.lastRefill).Seconds()
	if elapsed > 0 {
		tb.tokens += elapsed * tb.refillRate
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastRefill = now
	}

	if tb.tokens >= requested {
		tb.tokens -= requested
		return true, 0
	}

	// 令牌不足，计算补齐所需等待时长
	deficit := requested - tb.tokens
	secondsWait := deficit / tb.refillRate
	return false, time.Duration(secondsWait * float64(time.Second))
}
