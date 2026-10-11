package httpapi

import (
	"time"
)

// DeliveryRecordMeta 投递日志元数据
type DeliveryRecordMeta struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // processed, failed, dead_letter
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int       `json:"size_bytes"`
}

// RetentionPolicy 日志保留清理策略
type RetentionPolicy struct {
	MaxAgeDays      int  `json:"max_age_days"`
	KeepDeadLetters bool `json:"keep_dead_letters"`
	DryRun          bool `json:"dry_run"`
}

// PruneResult 日志清理统计结果
type PruneResult struct {
	TotalInspected int   `json:"total_inspected"`
	PrunedCount    int   `json:"pruned_count"`
	RetainedCount  int   `json:"retained_count"`
	ReclaimedBytes int64 `json:"reclaimed_bytes"`
	DryRun         bool  `json:"dry_run"`
}

// PruneDeliveryLogs 按照策略计算或执行日志保留清理
func PruneDeliveryLogs(records []DeliveryRecordMeta, policy RetentionPolicy, now time.Time) ([]DeliveryRecordMeta, PruneResult) {
	if policy.MaxAgeDays <= 0 {
		policy.MaxAgeDays = 30
	}

	cutoff := now.Add(-time.Duration(policy.MaxAgeDays) * 24 * time.Hour)

	var retained []DeliveryRecordMeta
	var prunedCount int
	var reclaimedBytes int64

	for _, r := range records {
		shouldPrune := false

		if r.CreatedAt.Before(cutoff) {
			if r.Status == "dead_letter" && policy.KeepDeadLetters {
				shouldPrune = false
			} else {
				shouldPrune = true
			}
		}

		if shouldPrune {
			prunedCount++
			reclaimedBytes += int64(r.SizeBytes)
			if policy.DryRun {
				retained = append(retained, r)
			}
		} else {
			retained = append(retained, r)
		}
	}

	result := PruneResult{
		TotalInspected: len(records),
		PrunedCount:    prunedCount,
		RetainedCount:  len(retained),
		ReclaimedBytes: reclaimedBytes,
		DryRun:         policy.DryRun,
	}

	return retained, result
}
