package httpapi

import (
	"testing"
	"time"
)

func TestPruneDeliveryLogs(t *testing.T) {
	now := time.Now()
	records := []DeliveryRecordMeta{
		{ID: "1", Status: "processed", CreatedAt: now.Add(-40 * 24 * time.Hour), SizeBytes: 1024},  // 超过30天，应清理
		{ID: "2", Status: "dead_letter", CreatedAt: now.Add(-35 * 24 * time.Hour), SizeBytes: 2048}, // 超过30天，但保留死信
		{ID: "3", Status: "processed", CreatedAt: now.Add(-10 * 24 * time.Hour), SizeBytes: 512},   // 未超期，保留
	}

	policy := RetentionPolicy{
		MaxAgeDays:      30,
		KeepDeadLetters: true,
		DryRun:          false,
	}

	retained, result := PruneDeliveryLogs(records, policy, now)

	if result.PrunedCount != 1 {
		t.Fatalf("expected 1 pruned record, got %d", result.PrunedCount)
	}
	if result.RetainedCount != 2 {
		t.Errorf("expected 2 retained records, got %d", result.RetainedCount)
	}
	if result.ReclaimedBytes != 1024 {
		t.Errorf("expected 1024 reclaimed bytes, got %d", result.ReclaimedBytes)
	}
	if len(retained) != 2 || retained[0].ID != "2" || retained[1].ID != "3" {
		t.Errorf("unexpected retained list: %+v", retained)
	}
}

func TestPruneDeliveryLogs_DryRun(t *testing.T) {
	now := time.Now()
	records := []DeliveryRecordMeta{
		{ID: "1", Status: "processed", CreatedAt: now.Add(-60 * 24 * time.Hour), SizeBytes: 500},
	}

	policy := RetentionPolicy{
		MaxAgeDays: 30,
		DryRun:     true,
	}

	retained, result := PruneDeliveryLogs(records, policy, now)
	if result.PrunedCount != 1 {
		t.Errorf("expected 1 pruned count in dry run, got %d", result.PrunedCount)
	}
	if len(retained) != 1 {
		t.Errorf("expected record not deleted in dry run, got %d", len(retained))
	}
}
