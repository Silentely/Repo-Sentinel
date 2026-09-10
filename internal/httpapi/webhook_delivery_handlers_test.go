package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestWebhookDeliveryHandlers(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := context.Background()
	now := time.Now().UTC()

	// 准备一条 delivery 记录
	d, err := fixture.store.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 "del-test-01",
		DeliveryID:         "gh-delivery-01",
		EventType:          "push",
		Action:             "",
		RepositoryFullName: "test/repo",
		Status:             store.DeliveryProcessed,
		Payload:            []byte(`{"ref":"refs/heads/main","commits":[]}`),
		ReceivedAt:         now,
	})
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}

	// 1. 未登录访问被拒绝
	unauth := fixture.request(t, http.MethodGet, "/api/v1/webhook-deliveries", "", "127.0.0.1:45101", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 2. 登录列表查询
	listResp := fixture.request(t, http.MethodGet, "/api/v1/webhook-deliveries?page=1&per_page=10", "", "127.0.0.1:45102", cookies, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listResp.Code, listResp.Body.String())
	}
	var listData struct {
		Items   []store.WebhookDelivery `json:"items"`
		Total   int                     `json:"total"`
		Page    int                     `json:"page"`
		PerPage int                     `json:"per_page"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listData); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if listData.Total < 1 || len(listData.Items) < 1 {
		t.Fatalf("expected at least 1 item, got %d", listData.Total)
	}

	// 3. 查询单条详情
	getResp := fixture.request(t, http.MethodGet, "/api/v1/webhook-deliveries/"+d.ID, "", "127.0.0.1:45103", cookies, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getResp.Code, getResp.Body.String())
	}
	var detailData struct {
		Delivery    store.WebhookDelivery `json:"delivery"`
		PayloadJSON map[string]any        `json:"payload_json"`
		PayloadRaw  string                `json:"payload_raw"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &detailData); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detailData.Delivery.DeliveryID != "gh-delivery-01" || detailData.PayloadJSON["ref"] != "refs/heads/main" {
		t.Fatalf("unexpected detail data: %+v", detailData)
	}

	// 4. 重放该 Webhook
	replayResp := fixture.request(
		t, http.MethodPost, "/api/v1/webhook-deliveries/"+d.ID+"/replay",
		"", "127.0.0.1:45104", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if replayResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", replayResp.Code, replayResp.Body.String())
	}
	var replayData struct {
		Status     string `json:"status"`
		DeliveryID string `json:"delivery_id"`
	}
	if err := json.Unmarshal(replayResp.Body.Bytes(), &replayData); err != nil {
		t.Fatalf("unmarshal replay: %v", err)
	}
	if replayData.Status != "replayed" || replayData.DeliveryID == "" {
		t.Fatalf("unexpected replay result: %+v", replayData)
	}
}

func TestActionsInsightsHandler(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	ctx := context.Background()
	now := time.Now().UTC()
	start1 := now.Add(-10 * time.Minute)
	end1 := now.Add(-8 * time.Minute) // 120s
	start2 := now.Add(-5 * time.Minute)
	end2 := now.Add(-4 * time.Minute) // 60s

	concSuccess := "success"
	concFailure := "failure"

	// 先插入一个 active 仓库
	_, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:         "r-1",
		Owner:      "test",
		Name:       "repo",
		FullName:   "test/repo",
		Type:       store.RepositoryTypeInstallation,
		SyncStatus: store.SyncStatusActive,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	_, _, _ = fixture.store.WorkflowRuns().UpsertIfNewer(ctx, store.WorkflowRun{
		ID: "wf-1", RepositoryID: "r-1", GitHubRunID: 101, WorkflowName: "CI Test",
		Conclusion: &concSuccess, RunStartedAt: &start1, RunCompletedAt: &end1,
	})
	_, _, _ = fixture.store.WorkflowRuns().UpsertIfNewer(ctx, store.WorkflowRun{
		ID: "wf-2", RepositoryID: "r-1", GitHubRunID: 102, WorkflowName: "CI Test",
		Conclusion: &concFailure, RunStartedAt: &start2, RunCompletedAt: &end2,
	})

	resp := fixture.request(t, http.MethodGet, "/api/v1/stats/actions-insights", "", "127.0.0.1:45201", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var insights ActionsInsightsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &insights); err != nil {
		t.Fatalf("unmarshal insights: %v", err)
	}

	if insights.TotalRuns != 2 || insights.SuccessRuns != 1 || insights.FailedRuns != 1 {
		t.Fatalf("unexpected runs count: %+v", insights)
	}
	if insights.SuccessRate != 50.0 {
		t.Fatalf("expected 50%% success rate, got %f", insights.SuccessRate)
	}
	if insights.AvgDurationSecs != 90.0 {
		t.Fatalf("expected avg 90s, got %f", insights.AvgDurationSecs)
	}
	if len(insights.TopFailing) != 1 || insights.TopFailing[0].Name != "CI Test" {
		t.Fatalf("unexpected top failing: %+v", insights.TopFailing)
	}
}
