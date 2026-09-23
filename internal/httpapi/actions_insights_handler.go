package httpapi

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// WorkflowStat 工作流级别统计。
type WorkflowStat struct {
	Name            string  `json:"name"`
	TotalRuns       int     `json:"total_runs"`
	FailedRuns      int     `json:"failed_runs"`
	FailureRate     float64 `json:"failure_rate"`
	AvgDurationSecs float64 `json:"avg_duration_seconds"`
}

// ActionsInsightsResponse Actions 效能洞察响应结构。
type ActionsInsightsResponse struct {
	TotalRuns          int            `json:"total_runs"`
	SuccessRuns        int            `json:"success_runs"`
	FailedRuns         int            `json:"failed_runs"`
	CancelledRuns      int            `json:"cancelled_runs"`
	SuccessRate        float64        `json:"success_rate"`
	AvgDurationSecs    float64        `json:"avg_duration_seconds"`
	MedianDurationSecs float64        `json:"median_duration_seconds"`
	P95DurationSecs    float64        `json:"p95_duration_seconds"`
	TopFailing         []WorkflowStat `json:"top_failing_workflows"`
}

// handleActionsInsights 提供 Actions 效能与 CI 稳定性/耗时分析。
func (s *server) handleActionsInsights(w http.ResponseWriter, r *http.Request) {
	repoID := strings.TrimSpace(r.URL.Query().Get("repository_id"))

	// 分析窗口取最近 actionsInsightsSampleSize 条运行：成功率/耗时分位数/失败 Top 都依赖样本量，
	// 窗口过小会让高频仓库的统计只剩几个小时、噪声掩盖真实趋势。
	runs, err := s.listRecentWorkflowRuns(r.Context(), repoID)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	resp := calculateActionsInsights(runs)
	writeJSON(w, http.StatusOK, resp)
}

// actionsInsightsSampleSize 效能洞察分析的运行样本量（窗口大小）。
// 列表接口 per_page 上限 100，故按页拉取到样本量为止。
const actionsInsightsSampleSize = 300

// listRecentWorkflowRuns 按页拉取最近若干条运行；末页不足一页即提前收尾（不做多余查询）。
func (s *server) listRecentWorkflowRuns(ctx context.Context, repoID string) ([]store.WorkflowRun, error) {
	var all []store.WorkflowRun
	for page := 1; len(all) < actionsInsightsSampleSize; page++ {
		batch, _, err := s.dependencies.Store.WorkflowRuns().List(ctx, store.ListFilter{
			Page:         page,
			PerPage:      100, // list filter per_page 上限 100
			RepositoryID: repoID,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	if len(all) > actionsInsightsSampleSize {
		all = all[:actionsInsightsSampleSize]
	}
	return all, nil
}

func calculateActionsInsights(runs []store.WorkflowRun) ActionsInsightsResponse {
	if len(runs) == 0 {
		return ActionsInsightsResponse{
			TopFailing: []WorkflowStat{},
		}
	}

	var total, success, failed, cancelled int
	var durations []float64

	type wfAcc struct {
		total     int
		failed    int
		durations []float64
	}
	wfMap := make(map[string]*wfAcc)

	for _, run := range runs {
		total++
		conclusion := ""
		if run.Conclusion != nil {
			conclusion = *run.Conclusion
		}

		if conclusion == "success" {
			success++
		} else if store.IsFailureConclusion(conclusion) {
			failed++
		} else if conclusion == "cancelled" {
			cancelled++
		}

		wfName := run.WorkflowName
		if wfName == "" {
			wfName = "Unknown Workflow"
		}
		if _, ok := wfMap[wfName]; !ok {
			wfMap[wfName] = &wfAcc{}
		}
		wfMap[wfName].total++
		if store.IsFailureConclusion(conclusion) {
			wfMap[wfName].failed++
		}

		if run.RunStartedAt != nil && run.RunCompletedAt != nil {
			dur := run.RunCompletedAt.Sub(*run.RunStartedAt).Seconds()
			if dur >= 0 {
				durations = append(durations, dur)
				wfMap[wfName].durations = append(wfMap[wfName].durations, dur)
			}
		}
	}

	sort.Float64s(durations)

	var avgDur, medianDur, p95Dur float64
	if len(durations) > 0 {
		var sum float64
		for _, d := range durations {
			sum += d
		}
		avgDur = math.Round((sum/float64(len(durations)))*10) / 10
		medianDur = math.Round(durations[len(durations)/2]*10) / 10
		p95Idx := int(float64(len(durations)) * 0.95)
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		p95Dur = math.Round(durations[p95Idx]*10) / 10
	}

	var successRate float64
	evaluatedRuns := success + failed
	if evaluatedRuns > 0 {
		successRate = math.Round((float64(success)/float64(evaluatedRuns))*1000) / 10
	} else if total > 0 {
		successRate = 100.0
	}

	topFailing := make([]WorkflowStat, 0)
	for name, acc := range wfMap {
		if acc.failed == 0 {
			continue
		}
		failRate := 0.0
		if acc.total > 0 {
			failRate = math.Round((float64(acc.failed)/float64(acc.total))*1000) / 10
		}
		var wfAvgDur float64
		if len(acc.durations) > 0 {
			var dsum float64
			for _, d := range acc.durations {
				dsum += d
			}
			wfAvgDur = math.Round((dsum/float64(len(acc.durations)))*10) / 10
		}
		topFailing = append(topFailing, WorkflowStat{
			Name:            name,
			TotalRuns:       acc.total,
			FailedRuns:      acc.failed,
			FailureRate:     failRate,
			AvgDurationSecs: wfAvgDur,
		})
	}

	// 优先按失败数降序，次之按失败率降序
	sort.Slice(topFailing, func(i, j int) bool {
		if topFailing[i].FailedRuns != topFailing[j].FailedRuns {
			return topFailing[i].FailedRuns > topFailing[j].FailedRuns
		}
		return topFailing[i].FailureRate > topFailing[j].FailureRate
	})

	if len(topFailing) > 5 {
		topFailing = topFailing[:5]
	}

	return ActionsInsightsResponse{
		TotalRuns:          total,
		SuccessRuns:        success,
		FailedRuns:         failed,
		CancelledRuns:      cancelled,
		SuccessRate:        successRate,
		AvgDurationSecs:    avgDur,
		MedianDurationSecs: medianDur,
		P95DurationSecs:    p95Dur,
		TopFailing:         topFailing,
	}
}
