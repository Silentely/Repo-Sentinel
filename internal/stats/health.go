package stats

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// DimensionScores 各维度得分。
type DimensionScores struct {
	IssueResponsiveness float64 `json:"issue_responsiveness"` // 满分 30
	PRVelocity          float64 `json:"pr_velocity"`          // 满分 30
	CIHealth            float64 `json:"ci_health"`            // 满分 20
	SecurityPosture     float64 `json:"security_posture"`     // 满分 20
}

// StagnantItem 停滞工单明细。
type StagnantItem struct {
	ID           string    `json:"id"`
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Kind         string    `json:"kind"`
	DaysStagnant int       `json:"days_stagnant"`
	HTMLURL      string    `json:"html_url"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// HealthReport 仓库健康度体检评分报告。
type HealthReport struct {
	Score         int             `json:"score"` // 0 - 100
	Grade         string          `json:"grade"` // A, B, C, D
	Summary       string          `json:"summary"`
	Dimensions    DimensionScores `json:"dimensions"`
	StagnantItems []StagnantItem  `json:"stagnant_items"`
}

// CalculateHealthScorecard 计算指定仓库或全量监控的健康度评分。
func CalculateHealthScorecard(
	ctx context.Context,
	st store.Store,
	repoID string,
	now time.Time,
) (HealthReport, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	filter := store.ListFilter{
		Page:         1,
		PerPage:      100,
		RepositoryID: repoID,
	}

	// 1. 抓取未关闭的活动工单
	wiFilter := filter
	wiFilter.State = "open"
	workItems, _, err := st.WorkItems().List(ctx, wiFilter)
	if err != nil {
		return HealthReport{}, err
	}

	// 2. 抓取最近 Workflow Runs
	workflowRuns, _, err := st.WorkflowRuns().List(ctx, filter)
	if err != nil {
		return HealthReport{}, err
	}

	// 3. 抓取未解决的安全告警
	alertFilter := filter
	alertFilter.State = "open"
	alerts, _, err := st.SecurityAlerts().List(ctx, alertFilter)
	if err != nil {
		return HealthReport{}, err
	}

	stagnantThreshold := now.Add(-7 * 24 * time.Hour)

	var openIssues, activeOpenIssues int
	var openPRs int
	var prStagnantDaysSum float64
	var stagnantItems []StagnantItem

	for _, item := range workItems {
		if item.Ignored || item.State != "open" {
			continue
		}

		activityTime := item.SourceUpdatedAt
		if activityTime.IsZero() {
			activityTime = item.UpdatedAt
		}
		isStagnant := activityTime.Before(stagnantThreshold)
		days := int(math.Max(0, now.Sub(activityTime).Hours()/24))

		if item.Kind == store.WorkItemKindPR {
			openPRs++
			prStagnantDaysSum += float64(days)
		} else {
			openIssues++
			if !isStagnant {
				activeOpenIssues++
			}
		}

		if isStagnant {
			stagnantItems = append(stagnantItems, StagnantItem{
				ID:           item.ID,
				Number:       item.Number,
				Title:        item.Title,
				Kind:         item.Kind,
				DaysStagnant: days,
				HTMLURL:      item.HTMLURL,
				UpdatedAt:    activityTime,
			})
		}
	}

	sort.Slice(stagnantItems, func(i, j int) bool {
		return stagnantItems[i].DaysStagnant > stagnantItems[j].DaysStagnant
	})

	// 维度 1: 工单响应度 (满分 30)
	var issueScore float64 = 30.0
	if openIssues > 0 {
		activeRate := float64(activeOpenIssues) / float64(openIssues)
		issueScore = math.Round(activeRate * 30.0)
	}

	// 维度 2: PR 流转效率 (满分 30)
	var prScore float64 = 30.0
	if openPRs > 0 {
		avgDays := prStagnantDaysSum / float64(openPRs)
		if avgDays <= 7 {
			prScore = 30.0
		} else if avgDays <= 14 {
			prScore = 20.0
		} else if avgDays <= 30 {
			prScore = 10.0
		} else {
			prScore = 0.0
		}
	}

	// 维度 3: CI 质量 (满分 20)
	var ciScore float64 = 20.0
	if len(workflowRuns) > 0 {
		var successCnt int
		evalRuns := min(len(workflowRuns), 20)
		for i := 0; i < evalRuns; i++ {
			if workflowRuns[i].Conclusion != nil && *workflowRuns[i].Conclusion == "success" {
				successCnt++
			}
		}
		rate := float64(successCnt) / float64(evalRuns)
		if rate >= 0.90 {
			ciScore = 20.0
		} else if rate >= 0.75 {
			ciScore = 15.0
		} else if rate >= 0.50 {
			ciScore = 10.0
		} else {
			ciScore = 0.0
		}
	}

	// 维度 4: 安全与告警 (满分 20)
	var secScore float64 = 20.0
	var criticalOrHighAlerts int
	for _, a := range alerts {
		if a.Ignored || a.State == "resolved" || a.State == "dismissed" {
			continue
		}
		sev := strings.ToLower(a.Severity)
		if sev == "critical" || sev == "high" {
			criticalOrHighAlerts++
		}
	}
	if criticalOrHighAlerts == 1 {
		secScore = 10.0
	} else if criticalOrHighAlerts >= 2 {
		secScore = 0.0
	}

	total := int(math.Round(issueScore + prScore + ciScore + secScore))
	if total < 0 {
		total = 0
	} else if total > 100 {
		total = 100
	}

	grade := "A"
	if total < 60 {
		grade = "D"
	} else if total < 75 {
		grade = "C"
	} else if total < 90 {
		grade = "B"
	}

	summary := "仓库健康状况极佳，各项指标流转健康。"
	if len(stagnantItems) > 0 {
		summary = "存在停滞工单待处理，建议跟进已超期未活动的 Issue 与 PR。"
	}
	if criticalOrHighAlerts > 0 {
		summary = "存在未解决的高危安全告警，需优先排查漏洞修复！"
	}

	return HealthReport{
		Score:   total,
		Grade:   grade,
		Summary: summary,
		Dimensions: DimensionScores{
			IssueResponsiveness: issueScore,
			PRVelocity:          prScore,
			CIHealth:            ciScore,
			SecurityPosture:     secScore,
		},
		StagnantItems: stagnantItems,
	}, nil
}
