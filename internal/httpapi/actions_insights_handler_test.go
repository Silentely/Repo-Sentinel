package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// seedWorkflowRuns 批量写入指定仓库的 workflow run；i 递增用于构造不同的更新时刻与结论。
func seedWorkflowRuns(t *testing.T, ctx context.Context, st store.Store, repoID string, count int) {
	t.Helper()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	success := "success"
	failure := "failure"
	for i := range count {
		conclusion := &success
		if i%5 == 0 {
			conclusion = &failure
		}
		started := base.Add(time.Duration(i) * time.Minute)
		completed := started.Add(90 * time.Second)
		if _, _, err := st.WorkflowRuns().UpsertIfNewer(ctx, store.WorkflowRun{
			ID:             fmt.Sprintf("wf-seed-%s-%d", repoID, i),
			RepositoryID:   repoID,
			GitHubRunID:    int64(1000 + i),
			WorkflowName:   "CI",
			RunNumber:      i + 1,
			Status:         "completed",
			Conclusion:     conclusion,
			RunStartedAt:   &started,
			RunCompletedAt: &completed,
			RunUpdatedAt:   started,
		}); err != nil {
			t.Fatalf("seed workflow run %d: %v", i, err)
		}
	}
}

func fetchInsights(t *testing.T, fixture *httpTestFixture, query string) ActionsInsightsResponse {
	t.Helper()
	path := "/api/v1/stats/actions-insights"
	if query != "" {
		path += "?" + query
	}
	resp := fixture.request(t, http.MethodGet, path, "", "127.0.0.1:45301", fixture.login(t, httpTestPassword), nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("actions-insights status=%d body=%s", resp.Code, resp.Body.String())
	}
	var insights ActionsInsightsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &insights); err != nil {
		t.Fatalf("unmarshal insights: %v", err)
	}
	return insights
}

// TestActionsInsightsAnalyzesFullSampleWindow 守护：效能洞察的统计窗口按 300 条样本取，
// 而非只取第一页 100 条——成功率与耗时分位数依赖样本量，窗口过小会让高频仓库的统计失真。
func TestActionsInsightsAnalyzesFullSampleWindow(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	ctx := t.Context()
	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-insight-1", Owner: "acme", Name: "insight", FullName: "acme/insight",
		Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	seedWorkflowRuns(t, ctx, fixture.store, "repo-insight-1", 250)

	insights := fetchInsights(t, fixture, "repository_id=repo-insight-1")
	if insights.TotalRuns != 250 {
		t.Fatalf("250 条运行应全部纳入分析窗口，got %d", insights.TotalRuns)
	}
	if insights.SuccessRuns+insights.FailedRuns+insights.CancelledRuns != 250 {
		t.Fatalf("结论分类应覆盖全部样本: %+v", insights)
	}
}

// TestActionsInsightsSampleWindowCappedAtLimit 守护：样本量超过窗口上限时截断到上限，
// 既不遗漏「最近」的时效性，也不让单次请求的查询次数随仓库运行数无界增长。
func TestActionsInsightsSampleWindowCappedAtLimit(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	ctx := t.Context()
	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-insight-2", Owner: "acme", Name: "insight", FullName: "acme/insight",
		Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	seedWorkflowRuns(t, ctx, fixture.store, "repo-insight-2", actionsInsightsSampleSize+50)

	insights := fetchInsights(t, fixture, "repository_id=repo-insight-2")
	if insights.TotalRuns != actionsInsightsSampleSize {
		t.Fatalf("样本应截断到窗口上限 %d，got %d", actionsInsightsSampleSize, insights.TotalRuns)
	}
}

// TestActionsInsightsScopedToRepository 守护：repository_id 过滤必须生效，
// 否则跨仓库汇总会把某一仓库的失败率摊薄到全量上，误导排障优先级。
func TestActionsInsightsScopedToRepository(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	ctx := t.Context()
	for _, id := range []string{"repo-insight-a", "repo-insight-b"} {
		if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
			ID: id, Owner: "acme", Name: id, FullName: "acme/" + id,
			Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		}); err != nil {
			t.Fatalf("upsert repo %s: %v", id, err)
		}
	}
	seedWorkflowRuns(t, ctx, fixture.store, "repo-insight-a", 20)
	seedWorkflowRuns(t, ctx, fixture.store, "repo-insight-b", 7)

	if got := fetchInsights(t, fixture, "repository_id=repo-insight-b").TotalRuns; got != 7 {
		t.Fatalf("应按仓库过滤，got %d", got)
	}
	if got := fetchInsights(t, fixture, "").TotalRuns; got != 27 {
		t.Fatalf("未指定仓库时应汇总全部，got %d", got)
	}
}
