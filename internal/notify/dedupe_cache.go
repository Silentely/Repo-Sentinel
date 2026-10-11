package notify

import (
	"sync"
	"time"
)

type cacheItem struct {
	expiresAt time.Time
}

// DedupeCache 轻量级内存去重缓存器
type DedupeCache struct {
	mu           sync.Mutex
	items        map[string]cacheItem
	duplicateHit int64
}

// NewDedupeCache 创建去重缓存器实例
func NewDedupeCache() *DedupeCache {
	return &DedupeCache{
		items: make(map[string]cacheItem),
	}
}

// CheckAndSet 检查并写入键。如果已存在且未过期返回 true (表示是重复项)；否则写入并返回 false
func (c *DedupeCache) CheckAndSet(key string, ttl time.Duration, now time.Time) (isDuplicate bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清理过期项或检查命中
	if item, exists := c.items[key]; exists {
		if now.Before(item.expiresAt) {
			c.duplicateHit++
			return true
		}
		// 已过期
		delete(c.items, key)
	}

	c.items[key] = cacheItem{
		expiresAt: now.Add(ttl),
	}
	return false
}

// PruneExpired 清理当前已过期的项
func (c *DedupeCache) PruneExpired(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	pruned := 0
	for k, item := range c.items {
		if !now.Before(item.expiresAt) {
			delete(c.items, k)
			pruned++
		}
	}
	return pruned
}

// DuplicateCount 获取累计去重命中次数
func (c *DedupeCache) DuplicateCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.duplicateHit
}
