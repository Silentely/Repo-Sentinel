package httpapi

import (
	"sync"
	"time"
)

// WebhookAuditRecord 记录 Webhook 请求鉴权与完整性审计详情
type WebhookAuditRecord struct {
	DeliveryID      string        `json:"delivery_id"`
	Event           string        `json:"event"`
	SenderIP        string        `json:"sender_ip"`
	PayloadSize     int           `json:"payload_size"`
	AuthDuration    time.Duration `json:"auth_duration"`
	VerificationOk  bool          `json:"verification_ok"`
	Reason          string        `json:"reason,omitempty"`
	Timestamp       time.Time     `json:"timestamp"`
}

// WebhookAuditLog 内存环形审计日志缓冲区
type WebhookAuditLog struct {
	mu       sync.RWMutex
	capacity int
	records  []WebhookAuditRecord
}

// NewWebhookAuditLog 创建审计缓冲区
func NewWebhookAuditLog(capacity int) *WebhookAuditLog {
	if capacity <= 0 {
		capacity = 100
	}
	return &WebhookAuditLog{
		capacity: capacity,
		records:  make([]WebhookAuditRecord, 0, capacity),
	}
}

// Record 记录一条审计信息
func (l *WebhookAuditLog) Record(rec WebhookAuditRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}

	if len(l.records) >= l.capacity {
		// 移除最早的一条
		l.records = l.records[1:]
	}
	l.records = append(l.records, rec)
}

// GetRecent 获取最近记录的审计列表
func (l *WebhookAuditLog) GetRecent(limit int) []WebhookAuditRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > len(l.records) {
		limit = len(l.records)
	}

	start := len(l.records) - limit
	result := make([]WebhookAuditRecord, limit)
	copy(result, l.records[start:])
	return result
}
