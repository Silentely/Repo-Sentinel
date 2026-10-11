package rules

import (
	"fmt"
	"math"
	"time"
)

// WorkflowRun 描述一次 CI/CD 工作流运行记录
type WorkflowRun struct {
	ID         string    `json:"id"`
	Workflow   string    `json:"workflow"`
	Status     string    `json:"status"`     // completed, in_progress, queued
	Conclusion string    `json:"conclusion"` // success, failure, cancelled, timed_out
	StartedAt  time.Time `json:"started_at"`
	Duration   time.Duration `json:"duration"`
}

// WorkflowMetrics 汇总工作流运行指标
type WorkflowMetrics struct {
	TotalRuns     int     `json:"total_runs"`
	SuccessRuns   int     `json:"success_runs"`
	FailedRuns    int     `json:"failed_runs"`
	CancelledRuns int     `json:"cancelled_runs"`
	SuccessRate   float64 `json:"success_rate"`   // 0.0 ~ 100.0%
	FailureRate   float64 `json:"failure_rate"`   // 0.0 ~ 100.0%
	AvgDuration   time.Duration `json:"avg_duration"`
	StatusBadge   string  `json:"status_badge"`   // 🟢 极佳, 🟡 需关注, 🔴 告警
}

// CalculateWorkflowMetrics 统计并评估指定时间窗口内的工作流指标
func CalculateWorkflowMetrics(runs []WorkflowRun, windowStart time.Time) WorkflowMetrics {
	if len(runs) == 0 {
		return WorkflowMetrics{
			StatusBadge: "⚪ 无运行记录",
		}
	}

	var totalDuration time.Duration
	var total, successes, failures, cancelled int

	for _, run := range runs {
		if !run.StartedAt.IsZero() && run.StartedAt.Before(windowStart) {
			continue
		}
		total++
		totalDuration += run.Duration
		switch run.Conclusion {
		case "success":
			successes++
		case "failure", "timed_out":
			failures++
		case "cancelled":
			cancelled++
		}
	}

	if total == 0 {
		return WorkflowMetrics{
			StatusBadge: "⚪ 窗口内无运行",
		}
	}

	sRate := math.Round((float64(successes)/float64(total))*1000) / 10
	fRate := math.Round((float64(failures)/float64(total))*1000) / 10
	avgDur := totalDuration / time.Duration(total)

	var badge string
	if sRate >= 95.0 {
		badge = fmt.Sprintf("🟢 极佳 (%.1f%%)", sRate)
	} else if sRate >= 80.0 {
		badge = fmt.Sprintf("🟡 需关注 (%.1f%%)", sRate)
	} else {
		badge = fmt.Sprintf("🔴 告警 (%.1f%%)", sRate)
	}

	return WorkflowMetrics{
		TotalRuns:     total,
		SuccessRuns:   successes,
		FailedRuns:    failures,
		CancelledRuns: cancelled,
		SuccessRate:   sRate,
		FailureRate:   fRate,
		AvgDuration:   avgDur,
		StatusBadge:   badge,
	}
}
