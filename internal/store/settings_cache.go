package store

import (
	"sync"
	"time"
)

// settingsCacheTTL 设置读取的进程内缓存时长。
// webhook 管线与调度器高频读取全局开关/运行参数（每次事件多处 Setting* 查询），
// 短 TTL 把热路径上的重复查询收敛为内存读取；5 秒与前端设置轮询粒度相当，
// 管理台改设置后 Upsert 会立即失效对应键，无需等待 TTL 到期。
const settingsCacheTTL = 5 * time.Second

// settingsCache 并发安全的短 TTL 设置缓存。
// 仅缓存存在且未过期的行；键不存在或已过期都视为未命中（不会缓存「不存在」，
// 避免插入新设置后读到过期结果）。
type settingsCache struct {
	mu   sync.Mutex
	ttl  time.Duration
	rows map[string]cachedSetting
}

type cachedSetting struct {
	row       SystemSetting
	expiresAt time.Time
}

func newSettingsCache(ttl time.Duration) *settingsCache {
	return &settingsCache{ttl: ttl, rows: make(map[string]cachedSetting)}
}

// Get 返回未过期缓存；nil 缓存（测试直构 settingsStore）视为未命中。
func (c *settingsCache) Get(key string) (SystemSetting, bool) {
	if c == nil {
		return SystemSetting{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.rows[key]
	if !ok || time.Now().After(item.expiresAt) {
		if ok {
			delete(c.rows, key)
		}
		return SystemSetting{}, false
	}
	return item.row, true
}

func (c *settingsCache) Set(key string, row SystemSetting) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows[key] = cachedSetting{row: row, expiresAt: time.Now().Add(c.ttl)}
}

// Invalidate 删除指定键，供 Upsert 写入后调用保证设置变更即时可见。
func (c *settingsCache) Invalidate(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.rows, key)
}
