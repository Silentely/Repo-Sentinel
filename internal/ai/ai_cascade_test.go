package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"testing"
)

func TestModelRoutingForTask(t *testing.T) {
	client := &Client{
		Model:      "gpt-4o-mini",
		FastModel:  "gemini-1.5-flash",
		HeavyModel: "gpt-4o",
	}

	// 1. Issue 分诊应路由至 Fast Model
	if got := client.RouteModelForTask(TaskTypeIssueTriage); got != "gemini-1.5-flash" {
		t.Errorf("expected FastModel gemini-1.5-flash for issue triage, got %s", got)
	}

	// 2. Release 总结应路由至 Fast Model
	if got := client.RouteModelForTask(TaskTypeReleaseNotes); got != "gemini-1.5-flash" {
		t.Errorf("expected FastModel gemini-1.5-flash for release notes, got %s", got)
	}

	// 3. PR 代码审查应路由至 Heavy Model
	if got := client.RouteModelForTask(TaskTypePRReview); got != "gpt-4o" {
		t.Errorf("expected HeavyModel gpt-4o for pr review, got %s", got)
	}

	// 4. 未配置 FastModel 时应平稳回退至主模型
	clientFallback := &Client{
		Model: "gpt-4o-mini",
	}
	if got := clientFallback.RouteModelForTask(TaskTypeIssueTriage); got != "gpt-4o-mini" {
		t.Errorf("expected fallback to gpt-4o-mini, got %s", got)
	}
}

func TestAICascade_AtomicBudgetUpsertAndThrottle(t *testing.T) {
	st := openRuntimeStore(t)
	ctx := context.Background()

	todayKey := "ai_budget:2026-10-05-test"
	budgetLimitCents := 100 // 预算上限 100 美分 ($1.00)

	// Step 1: 首次调用自动建行测试（P0 防线：验证 updated_by 哨兵值与建行）
	throttled, err := st.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, 500, 10, budgetLimitCents)
	if err != nil {
		t.Fatalf("first call atomic upsert failed: %v", err)
	}
	if throttled {
		t.Errorf("expected throttled=false on first call with 10 cents vs 100 limit")
	}

	// 验证 system_settings 表中该行已建立且 updated_by 为 ai_budget
	setting, err := st.Settings().Get(ctx, todayKey)
	if err != nil {
		t.Fatalf("get setting failed: %v", err)
	}
	if setting.UpdatedBy != "ai_budget" {
		t.Errorf("expected updated_by to be ai_budget, got %s", setting.UpdatedBy)
	}

	// Step 2: 并发 10 次累加使成本超限
	// 每次累加 15 美分，10 次为 150 美分，累计 160 美分 > 100 美分
	var wg sync.WaitGroup
	var throttledCount int
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			isThrottled, updateErr := st.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, 200, 15, budgetLimitCents)
			if updateErr != nil {
				t.Errorf("concurrent update failed: %v", updateErr)
				return
			}
			if isThrottled {
				mu.Lock()
				throttledCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if throttledCount == 0 {
		t.Fatalf("expected throttled to trigger when exceeding budget, but throttledCount=0")
	}

	// 最终查询必须是 throttled
	finalThrottled, err := st.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, 10, 1, budgetLimitCents)
	if err != nil {
		t.Fatalf("final check failed: %v", err)
	}
	if !finalThrottled {
		t.Errorf("expected finalThrottled=true, got false")
	}
}

func TestAICascade_PostgresBudgetAtomic(t *testing.T) {
	pgURL := os.Getenv("REPOSENTINEL_TEST_POSTGRES_URL")
	if pgURL == "" {
		t.Skip("未设置 REPOSENTINEL_TEST_POSTGRES_URL，跳过 PostgreSQL 预算原子累加测试")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, config.DatabaseConfig{
		Driver: "postgres",
		URL:    pgURL,
	})
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	defer st.Close()

	todayKey := fmt.Sprintf("ai_budget_pg_test_%d", time.Now().UnixNano())
	budgetLimitCents := 50

	throttled, err := st.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, 100, 10, budgetLimitCents)
	if err != nil {
		t.Fatalf("first call failed on pg: %v", err)
	}
	if throttled {
		t.Errorf("expected not throttled on first call")
	}

	throttled, err = st.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, 500, 45, budgetLimitCents)
	if err != nil {
		t.Fatalf("second call failed on pg: %v", err)
	}
	if !throttled {
		t.Errorf("expected throttled on pg when cost (55) >= limit (50)")
	}
}

func TestCascade_ModelSelectionEndToEnd(t *testing.T) {
	var lastModel string
	var mu sync.Mutex

	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"content":"{\"summary\":\"ok\",\"score\":80,\"confidence\":4,\"category\":\"Feature\",\"merge_risk\":\"Low\",\"maintainer_verdict\":\"Ready to Merge\",\"security_risks\":[],\"breaking_risks\":[],\"code_smells\":[]}"}}]}`, func(r *http.Request, raw []byte) {
		var req chatRequest
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		lastModel = req.Model
		mu.Unlock()
	})
	defer srv.Close()

	client := &Client{
		BaseURL:               srv.URL,
		APIKey:                "k",
		Model:                 "default-model",
		FastModel:             "fast-model",
		HeavyModel:            "heavy-model",
		Enabled:               true,
		TriageEnabled:         true,
		CodeReviewEnabled:     true,
		ReleaseSummaryEnabled: true,
	}

	// 1. TriageAlert 应使用 fast-model
	_, _ = client.TriageAlert(t.Context(), store.Event{Kind: "dependabot", Title: "test"}, "repo/test")
	mu.Lock()
	if lastModel != "fast-model" {
		t.Errorf("expected fast-model for TriageAlert, got %s", lastModel)
	}
	mu.Unlock()

	// 2. ReviewPR 应使用 heavy-model
	_, _ = client.ReviewPR(t.Context(), "repo/test", "fix sql injection", "alice", "diff --git a/a.go b/a.go")
	mu.Lock()
	if lastModel != "heavy-model" {
		t.Errorf("expected heavy-model for ReviewPR, got %s", lastModel)
	}
	mu.Unlock()

	// 3. 通用 Complete 应回退到 default-model
	_, _ = client.Complete(t.Context(), "sys", "usr")
	mu.Lock()
	if lastModel != "default-model" {
		t.Errorf("expected default-model for general Complete, got %s", lastModel)
	}
	mu.Unlock()
}
