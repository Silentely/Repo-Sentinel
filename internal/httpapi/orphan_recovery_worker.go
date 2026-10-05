package httpapi

import (
	"context"
	"time"
)

// OrphanRecoveryConfig 孤儿恢复配置。
type OrphanRecoveryConfig struct {
	Interval        time.Duration
	BatchLimit      int
	AcceptedAge     time.Duration
	MaxAttempts     int
	MaxRetentionAge time.Duration
}

func defaultOrphanRecoveryConfig() OrphanRecoveryConfig {
	return OrphanRecoveryConfig{
		Interval:        30 * time.Second,
		BatchLimit:      50,
		AcceptedAge:     2 * time.Minute,
		MaxAttempts:     5,
		MaxRetentionAge: 30 * time.Minute,
	}
}

// startOrphanRecoveryWorker 启动孤儿 Webhook 恢复与死信分流的常驻后台 Worker。
func (s *server) startOrphanRecoveryWorker(ctx context.Context) {
	s.safeGo("webhook_orphan_recovery", func() {
		s.runOrphanRecoveryLoop(ctx, 30*time.Second)
	})
}

// runOrphanRecoveryLoop 持续运行孤儿恢复与死信分流循环。
func (s *server) runOrphanRecoveryLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOrphanRecoveryOnce(ctx, defaultOrphanRecoveryConfig())
		}
	}
}

// runOrphanRecoveryOnce 执行单次孤儿扫描与抢占恢复。
func (s *server) runOrphanRecoveryOnce(ctx context.Context, cfg OrphanRecoveryConfig) int {
	if s.dependencies.Store == nil || s.dependencies.Store.WebhookDeliveries() == nil {
		return 0
	}
	workerID := s.getWorkerID()
	cutoff := time.Now().UTC().Add(-cfg.AcceptedAge)

	claimed, err := s.dependencies.Store.WebhookDeliveries().ClaimDueOrphanWebhooks(ctx, workerID, cutoff, cfg.BatchLimit)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("orphan webhook claim error", "error", err.Error())
		}
		return 0
	}

	recoveredCount := 0
	now := time.Now().UTC()
	for _, delivery := range claimed {
		// 死信判定：尝试次数达到上限，或滞留时间超过上限
		if delivery.AttemptCount >= cfg.MaxAttempts || now.Sub(delivery.ReceivedAt) > cfg.MaxRetentionAge {
			reason := "max_attempts_exceeded"
			if now.Sub(delivery.ReceivedAt) > cfg.MaxRetentionAge {
				reason = "retention_timeout_exceeded"
			}
			res, markErr := s.dependencies.Store.WebhookDeliveries().MarkDeadLetter(ctx, delivery.ID, delivery.ClaimToken, reason)
			if markErr == nil && res.Applied {
				MetricsIncWebhookDeadLetter()
				if s.dependencies.Logger != nil {
					s.dependencies.Logger.Warn("webhook moved to dead letter",
						"delivery_id", delivery.DeliveryID,
						"id", delivery.ID,
						"attempt_count", delivery.AttemptCount,
						"reason", reason,
					)
				}
			}
			continue
		}

		// 正常恢复调度
		recoveredCount++
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Info("orphan webhook recovered for processing",
				"delivery_id", delivery.DeliveryID,
				"id", delivery.ID,
				"attempt_count", delivery.AttemptCount,
				"worker_id", workerID,
			)
		}
		s.processWebhookAsync(delivery.ID, delivery.EventType, delivery.DeliveryID, delivery.ClaimToken, delivery.Payload)
	}
	return recoveredCount
}
