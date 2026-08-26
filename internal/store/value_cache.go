package store

import (
	"sync"
	"time"
)

// ttlValueCache 并发安全的单值短 TTL 缓存。
// 供「高频读取、低频变更」的查询结果（渠道列表、仪表盘统计、仓库 ID 集合）进程内复用，
// 与 settingsCache 同一约定：TTL 与前端轮询粒度相当，写入路径调 Invalidate 保证变更即时可见。
// 返回的切片/结构为共享引用，调用方不得改写（只读消费，与 settingsCache 相同）。
type ttlValueCache[T any] struct {
	mu        sync.Mutex
	ttl       time.Duration
	value     T
	fresh     bool
	expiresAt time.Time
}

func newTTLValueCache[T any](ttl time.Duration) *ttlValueCache[T] {
	return &ttlValueCache[T]{ttl: ttl}
}

// Get 返回未过期缓存值；nil 缓存（测试直构存储访问器）视为未命中。
func (c *ttlValueCache[T]) Get() (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fresh || time.Now().After(c.expiresAt) {
		c.fresh = false
		return zero, false
	}
	return c.value, true
}

func (c *ttlValueCache[T]) Set(v T) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = v
	c.fresh = true
	c.expiresAt = time.Now().Add(c.ttl)
}

// Invalidate 丢弃缓存值，供写入路径调用保证变更即时可见。
func (c *ttlValueCache[T]) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fresh = false
}

// cachedRepoIDs 一次仓库表扫描分出的两个 ID 集合。
type cachedRepoIDs struct {
	active   []string
	archived []string
}

// dashboardCacheTTL 仪表盘统计缓存时长：统计容忍秒级陈旧（前端 30s 轮询），
// 不做逐写失效；比设置缓存更短，保证操作反馈在下一两次轮询内可见。
const dashboardCacheTTL = 3 * time.Second
