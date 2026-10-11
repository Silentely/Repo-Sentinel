package rules

import (
	"testing"
	"time"
)

func TestCalculateWorkflowMetrics(t *testing.T) {
	now := time.Now()
	runs := []WorkflowRun{
		{ID: "1", Conclusion: "success", StartedAt: now.Add(-1 * time.Hour), Duration: 2 * time.Minute},
		{ID: "2", Conclusion: "success", StartedAt: now.Add(-2 * time.Hour), Duration: 4 * time.Minute},
		{ID: "3", Conclusion: "failure", StartedAt: now.Add(-3 * time.Hour), Duration: 3 * time.Minute},
		{ID: "4", Conclusion: "cancelled", StartedAt: now.Add(-4 * time.Hour), Duration: 1 * time.Minute},
		{ID: "5", Conclusion: "success", StartedAt: now.Add(-48 * time.Hour), Duration: 5 * time.Minute}, // 超出24小时窗口
	}

	windowStart := now.Add(-24 * time.Hour)
	metrics := CalculateWorkflowMetrics(runs, windowStart)

	if metrics.TotalRuns != 4 {
		t.Fatalf("expected total runs 4, got %d", metrics.TotalRuns)
	}
	if metrics.SuccessRuns != 2 {
		t.Errorf("expected success runs 2, got %d", metrics.SuccessRuns)
	}
	if metrics.FailedRuns != 1 {
		t.Errorf("expected failed runs 1, got %d", metrics.FailedRuns)
	}
	if metrics.CancelledRuns != 1 {
		t.Errorf("expected cancelled runs 1, got %d", metrics.CancelledRuns)
	}
	// 2/4 = 50.0%
	if metrics.SuccessRate != 50.0 {
		t.Errorf("expected success rate 50.0, got %f", metrics.SuccessRate)
	}
	if metrics.StatusBadge == "" || metrics.StatusBadge[0:4] != "🔴" {
		t.Errorf("expected warning badge starting with 🔴, got %s", metrics.StatusBadge)
	}
}

func TestCalculateWorkflowMetrics_Empty(t *testing.T) {
	metrics := CalculateWorkflowMetrics(nil, time.Now())
	if metrics.TotalRuns != 0 {
		t.Errorf("expected 0 total runs, got %d", metrics.TotalRuns)
	}
}
