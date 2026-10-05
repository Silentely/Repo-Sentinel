package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// TestChatOpsE2E 端到端测试：
// 1. Actions 失败事件触发规则引擎，成功在 Outbox 的 BodyJSON 中附带 chatops_token；
// 2. 模拟 Telegram / Feishu 触发回调，核销 Token 并调用 GitHub Rerun API；
// 3. 连续快速重复点击回调，断言第二次无法再次核销（防重放与防双击）；
// 4. 伪造签名与过期 Token 安全拦截。
func TestChatOpsE2E(t *testing.T) {
	// 4. 模拟 GitHub API 收到 Rerun 请求并打桩
	var rerunCallCount int64
	mockGitHubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/sentinel-repo/actions/runs/987654/rerun" || r.URL.Path == "/repos/acme/sentinel-repo/actions/runs/987655/rerun" {
			atomic.AddInt64(&rerunCallCount, 1)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockGitHubServer.Close()

	ghClient := githubx.NewAppClient(1, "")
	ghClient.BaseURL = mockGitHubServer.URL
	ghClient.HTTP = mockGitHubServer.Client()

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})

	// 1. 注册仓库与通知渠道
	repo, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             ulid.Make().String(),
		Owner:          "acme",
		Name:           "sentinel-repo",
		FullName:       "acme/sentinel-repo",
		DefaultBranch:  "main",
		MonitorEnabled: true,
		ActionsEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	_, err = fixture.store.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID:          ulid.Make().String(),
		ChannelType: store.ChannelTelegram,
		Target:      "12345678",
		Enabled:     true,
		EventKinds:  []string{store.WorkflowRunKind},
	})
	if err != nil {
		t.Fatalf("创建通知渠道失败: %v", err)
	}

	// 2. 模拟 Actions Workflow 失败事件，送入规则引擎
	runID := int64(987654)
	event := &store.Event{
		ID:                 ulid.Make().String(),
		Source:             "webhook",
		Kind:               store.WorkflowRunKind,
		Action:             "completed",
		RepositoryID:       &repo.ID,
		WorkflowRunID:      &runID,
		Title:              "CI Pipeline",
		Actor:              "developer-alice",
		WorkflowConclusion: "failure",
		OccurredAt:         time.Now().UTC(),
		HTMLURL:            "https://github.com/acme/sentinel-repo/actions/runs/987654",
	}

	engine := &rules.Engine{
		Store: fixture.store,
	}

	err = engine.Evaluate(t.Context(), normalizer.Result{
		Event:      event,
		Repository: &repo,
	}, repo.FullName)
	if err != nil {
		t.Fatalf("规则引擎评估失败: %v", err)
	}

	// 3. 检查 Outbox 中的消息，断言 BodyJSON 中成功携带 chatops_token
	outboxItems, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{PerPage: 10})
	if err != nil {
		t.Fatalf("读取 Outbox 失败: %v", err)
	}
	if len(outboxItems) == 0 {
		t.Fatal("期望产生一条 Outbox 失败通知，实际为 0")
	}

	item := outboxItems[0]
	tokenVal, ok := item.BodyJSON["chatops_token"].(string)
	if !ok || tokenVal == "" {
		t.Fatalf("BodyJSON 必须携带有效 chatops_token: %v", item.BodyJSON)
	}
	actionVal, _ := item.BodyJSON["chatops_action"].(string)
	if actionVal != "workflow_rerun" {
		t.Fatalf("chatops_action 期望为 workflow_rerun, 实际: %s", actionVal)
	}

	// 5. 模拟 Telegram Callback 快速重试点击
	tgBody := map[string]any{
		"update_id": 1,
		"callback_query": map[string]any{
			"id":   "cb-1",
			"data": "rerun:" + tokenVal,
			"from": map[string]any{
				"id":       12345678,
				"username": "alice",
			},
		},
	}
	tgJSON, _ := json.Marshal(tgBody)

	// 第 1 次点击回调：成功核销并执行重试
	req1 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(tgJSON))
	rec1 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("第 1 次 Telegram 回调期望 200, 实际: %d, body: %s", rec1.Code, rec1.Body.String())
	}
	if calls := atomic.LoadInt64(&rerunCallCount); calls != 1 {
		t.Fatalf("GitHub Rerun API 期望调用 1 次，实际: %d", calls)
	}

	// 第 2 次快速重复点击：防双击 / 防重放拦截
	req2 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(tgJSON))
	rec2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec2, req2)

	var secondResp map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &secondResp)
	if secondResp["status"] != "ignored" {
		t.Fatalf("第 2 次点击回调必须被忽略并标记为 status=ignored, 实际: %v", secondResp)
	}
	if calls := atomic.LoadInt64(&rerunCallCount); calls != 1 {
		t.Fatalf("第二次重复点击不应再次调用 GitHub API, 实际调用: %d 次", calls)
	}

	// 6. 验证过期 Token 安全拦截
	expiredTokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", repo.ID, strconv.FormatInt(runID, 10), "user-1", -1*time.Minute)
	if err != nil {
		t.Fatalf("创建过期 Token 失败: %v", err)
	}
	expiredTgBody := map[string]any{
		"update_id": 2,
		"callback_query": map[string]any{
			"id":   "cb-2",
			"data": "rerun:" + expiredTokenID,
			"from": map[string]any{
				"id":       12345678,
				"username": "alice",
			},
		},
	}
	expJSON, _ := json.Marshal(expiredTgBody)
	reqExp := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(expJSON))
	recExp := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recExp, reqExp)

	var expResp map[string]any
	_ = json.Unmarshal(recExp.Body.Bytes(), &expResp)
	if expResp["status"] != "ignored" || expResp["reason"] != "token expired" {
		t.Fatalf("过期 Token 必须被安全拦截为 token expired, 实际: %v", expResp)
	}

	// 7. 验证 Feishu 回调解析携带 chatops_token
	runID2 := int64(987655)
	feishuTokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", repo.ID, strconv.FormatInt(runID2, 10), "", 10*time.Minute)
	if err != nil {
		t.Fatalf("创建 Feishu Token 失败: %v", err)
	}
	feishuBody := map[string]any{
		"action": map[string]any{
			"value": map[string]any{
				"action":        "workflow_rerun",
				"chatops_token": feishuTokenID,
			},
		},
	}
	feishuJSON, _ := json.Marshal(feishuBody)
	reqFeishu := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(feishuJSON))
	recFeishu := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recFeishu, reqFeishu)

	if recFeishu.Code != http.StatusOK {
		t.Fatalf("Feishu 回调期望 200, 实际: %d, body: %s", recFeishu.Code, recFeishu.Body.String())
	}
	if calls := atomic.LoadInt64(&rerunCallCount); calls != 2 {
		t.Fatalf("Feishu 回调成功执行后总 Rerun 次数期望为 2, 实际: %d", calls)
	}
}
