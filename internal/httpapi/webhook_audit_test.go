package httpapi

import (
	"testing"
	"time"
)

func TestWebhookAuditLog(t *testing.T) {
	// 容量为 3 的审计缓冲
	log := NewWebhookAuditLog(3)

	for i := 1; i <= 5; i++ {
		log.Record(WebhookAuditRecord{
			DeliveryID:     "deliv-00",
			Event:          "push",
			SenderIP:       "192.168.1.1",
			PayloadSize:    1024 * i,
			AuthDuration:   time.Duration(i) * time.Millisecond,
			VerificationOk: true,
		})
	}

	recent := log.GetRecent(10)
	if len(recent) != 3 {
		t.Fatalf("expected ring buffer capped at 3, got %d", len(recent))
	}

	// 最新的应该是第 5 条 (PayloadSize = 5120)
	if recent[2].PayloadSize != 5120 {
		t.Errorf("expected last record payload size 5120, got %d", recent[2].PayloadSize)
	}

	sub := log.GetRecent(2)
	if len(sub) != 2 {
		t.Fatalf("expected 2 records, got %d", len(sub))
	}
}
