package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ContentDeduplicator 基于渠道与消息内容的短期内存去重器，防止短时间内因重复 Webhook 或定时任务重试产生幽灵通知。
type ContentDeduplicator struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

// NewContentDeduplicator 创建内容去重器，默认 TTL（如 30 秒或 1 分钟）。
func NewContentDeduplicator(ttl time.Duration) *ContentDeduplicator {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &ContentDeduplicator{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// DefaultContentDeduplicator 全局默认去重器（TTL 为 45 秒）。
var DefaultContentDeduplicator = NewContentDeduplicator(45 * time.Second)

// Fingerprint 计算渠道与消息内容的指纹。
func Fingerprint(channelID, title, body string) string {
	h := sha256.New()
	h.Write([]byte(channelID))
	h.Write([]byte{':'})
	h.Write([]byte(title))
	h.Write([]byte{':'})
	h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ShouldSuppress 判断当前消息是否在短期内已发送过完全一致的内容。
// 若已发送过且未过期则返回 true；否则记录该指纹并返回 false。
func (d *ContentDeduplicator) ShouldSuppress(channelID, title, body string, now time.Time) bool {
	fp := Fingerprint(channelID, title, body)
	d.mu.Lock()
	defer d.mu.Unlock()

	// 清理过期条目
	for k, exp := range d.seen {
		if now.After(exp) {
			delete(d.seen, k)
		}
	}

	if exp, exists := d.seen[fp]; exists {
		if now.Before(exp) {
			return true
		}
	}

	d.seen[fp] = now.Add(d.ttl)
	return false
}
