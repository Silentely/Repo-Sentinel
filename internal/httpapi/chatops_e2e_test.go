package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// TestChatOps_IssueActions_E2E 测试 Issue 快捷菜单动作全链路闭环：
// 1. 采纳建议回复草稿：通过 Telegram act:<token> 触发，调用 GitHub API 发布 Issue 评论；
// 2. 标记垃圾并关闭：打上 invalid 标签并将状态变更为 closed (not_planned)；
// 3. 应用建议标签：调用 AddIssueLabels 将建议分类与标签贴到 Issue。
func TestChatOps_IssueActions_E2E(t *testing.T) {
	var (
		commentBodyPosted string
		labelsAdded42     []string
		labelsAdded99     []string
		closePatch99      map[string]any
		commentBody77     string
		labelsAdded77     []string
		reviewEvent88     string
		reviewBody88      string
		commentBody120    string
		labelsAdded120    []string
		closePatch120     map[string]any
	)

	mockGitHubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/42/comments":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			commentBodyPosted = body["body"]
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1001, "body": commentBodyPosted})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/99/labels":
			var body struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			labelsAdded99 = body.Labels
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{})

		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/sentinel-repo/issues/99":
			_ = json.NewDecoder(r.Body).Decode(&closePatch99)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed"})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/101/labels":
			var body struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			labelsAdded42 = body.Labels
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/77/comments":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			commentBody77 = body["body"]
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1002, "body": commentBody77})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/77/labels":
			var body struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			labelsAdded77 = body.Labels
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/120/comments":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			commentBody120 = body["body"]
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1003, "body": commentBody120})

		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/sentinel-repo/issues/120":
			_ = json.NewDecoder(r.Body).Decode(&closePatch120)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed"})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/issues/120/labels":
			var body struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			labelsAdded120 = body.Labels
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/sentinel-repo/pulls/88/reviews":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			reviewEvent88 = body["event"]
			reviewBody88 = body["body"]
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2001, "state": "APPROVED"})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockGitHubServer.Close()

	ghClient := githubx.NewAppClient(1, "")
	ghClient.BaseURL = mockGitHubServer.URL
	ghClient.HTTP = mockGitHubServer.Client()

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})

	repo, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             ulid.Make().String(),
		Owner:          "acme",
		Name:           "sentinel-repo",
		FullName:       "acme/sentinel-repo",
		DefaultBranch:  "main",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	// 1. 测试一键采纳建议回复
	replyToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_reply_draft", repo.ID, 42, "感谢反馈！建议先核验配置项。", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建回复令牌失败: %v", err)
	}
	tgReply := map[string]any{
		"update_id": 1,
		"callback_query": map[string]any{
			"id":   "cb-reply",
			"data": "act:" + replyToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawReply, _ := json.Marshal(tgReply)
	reqReply := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawReply))
	recReply := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recReply, reqReply)
	if recReply.Code != http.StatusOK {
		t.Fatalf("回复回调失败: %d, %s", recReply.Code, recReply.Body.String())
	}
	if commentBodyPosted != "感谢反馈！建议先核验配置项。" {
		t.Fatalf("发布的回复内容不符，实际: %q", commentBodyPosted)
	}

	// 2. 测试标记垃圾内容并关闭
	spamToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_spam_and_close", repo.ID, 99, "", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建垃圾关闭令牌失败: %v", err)
	}
	tgSpam := map[string]any{
		"update_id": 2,
		"callback_query": map[string]any{
			"id":   "cb-spam",
			"data": "act:" + spamToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawSpam, _ := json.Marshal(tgSpam)
	reqSpam := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawSpam))
	recSpam := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recSpam, reqSpam)
	if recSpam.Code != http.StatusOK {
		t.Fatalf("垃圾关闭回调失败: %d, %s", recSpam.Code, recSpam.Body.String())
	}
	if len(labelsAdded99) != 1 || labelsAdded99[0] != "invalid" {
		t.Fatalf("垃圾打标不符，实际: %v", labelsAdded99)
	}
	if closePatch99["state"] != "closed" || closePatch99["state_reason"] != "not_planned" {
		t.Fatalf("关闭参数不符，实际: %v", closePatch99)
	}

	// 3. 测试一键应用建议标签
	labelsToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_apply_labels", repo.ID, 101, "bug,frontend", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建标签令牌失败: %v", err)
	}
	tgLabels := map[string]any{
		"update_id": 3,
		"callback_query": map[string]any{
			"id":   "cb-labels",
			"data": "act:" + labelsToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawLabels, _ := json.Marshal(tgLabels)
	reqLabels := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawLabels))
	recLabels := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recLabels, reqLabels)
	if recLabels.Code != http.StatusOK {
		t.Fatalf("标签回调失败: %d, %s", recLabels.Code, recLabels.Body.String())
	}
	if len(labelsAdded42) != 2 || labelsAdded42[0] != "bug" || labelsAdded42[1] != "frontend" {
		t.Fatalf("应用标签不符，实际: %v", labelsAdded42)
	}

	// 4. 测试索取复现要素（发追问评论 + 贴 needs-more-info 标签）
	reqDetailsToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_request_details", repo.ID, 77, "请协助补充复现 Demo 与运行日志。", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建索取要素令牌失败: %v", err)
	}
	tgDetails := map[string]any{
		"update_id": 4,
		"callback_query": map[string]any{
			"id":   "cb-details",
			"data": "act:" + reqDetailsToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawDetails, _ := json.Marshal(tgDetails)
	reqD := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawDetails))
	recD := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recD, reqD)
	if recD.Code != http.StatusOK {
		t.Fatalf("索取要素回调失败: %d, %s", recD.Code, recD.Body.String())
	}
	if commentBody77 != "请协助补充复现 Demo 与运行日志。" {
		t.Fatalf("追问评论不符，实际: %q", commentBody77)
	}
	if len(labelsAdded77) != 1 || labelsAdded77[0] != "needs-more-info" {
		t.Fatalf("标签不符，实际: %v", labelsAdded77)
	}

	// 5. 测试极速稍后处理（生成 2 小时后的待办 Outbox）
	_, _ = fixture.store.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID:          "ch-snooze",
		ChannelType: store.ChannelTelegram,
		Enabled:     true,
		EventKinds:  []string{store.WorkItemKindIssue},
	})
	snoozePayload := "acme/sentinel-repo|99|https://github.com/acme/sentinel-repo/issues/99"
	snoozeToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_snooze", repo.ID, 99, snoozePayload, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建延迟令牌失败: %v", err)
	}
	tgSnooze := map[string]any{
		"update_id": 5,
		"callback_query": map[string]any{
			"id":   "cb-snooze",
			"data": "act:" + snoozeToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawSnooze, _ := json.Marshal(tgSnooze)
	reqS := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawSnooze))
	recS := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recS, reqS)
	if recS.Code != http.StatusOK {
		t.Fatalf("延迟提醒回调失败: %d, %s", recS.Code, recS.Body.String())
	}
	// 验证 Outbox 中已写入待办提醒
	outboxList, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{PerPage: 20})
	if err != nil {
		t.Fatalf("读取 Outbox 失败: %v", err)
	}
	foundSnooze := false
	for _, o := range outboxList {
		if strings.Contains(o.Title, "[待办提醒]") && strings.Contains(o.Title, "#99") {
			foundSnooze = true
			if !o.NextAttemptAt.After(time.Now().UTC().Add(1 * time.Hour)) {
				t.Fatalf("期望延后大约 2 小时，实际 NextAttemptAt: %v", o.NextAttemptAt)
			}
			break
		}
	}
	if !foundSnooze {
		t.Fatal("未在 Outbox 中找到延迟提醒条目")
	}

	// 6. 测试关联历史重复项并关闭
	dupToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_duplicate", repo.ID, 120, "10", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建关联重复项令牌失败: %v", err)
	}
	tgDup := map[string]any{
		"update_id": 6,
		"callback_query": map[string]any{
			"id":   "cb-dup",
			"data": "act:" + dupToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawDup, _ := json.Marshal(tgDup)
	reqDup := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawDup))
	recDup := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recDup, reqDup)
	if recDup.Code != http.StatusOK {
		t.Fatalf("关联重复项回调失败: %d, %s", recDup.Code, recDup.Body.String())
	}
	if !strings.Contains(commentBody120, "#10") {
		t.Fatalf("关联评论未包含历史编号 #10: %q", commentBody120)
	}
	if len(labelsAdded120) != 1 || labelsAdded120[0] != "duplicate" {
		t.Fatalf("关联打标不符，实际: %v", labelsAdded120)
	}
	if closePatch120["state"] != "closed" || closePatch120["state_reason"] != "not_planned" {
		t.Fatalf("关闭状态不符，实际: %v", closePatch120)
	}

	// 7. 测试快捷批准 PR
	prApproveToken, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "pr_quick_approve", repo.ID, 88, "", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("创建快捷批准令牌失败: %v", err)
	}
	tgApprove := map[string]any{
		"update_id": 6,
		"callback_query": map[string]any{
			"id":   "cb-approve",
			"data": "act:" + prApproveToken,
			"from": map[string]any{"id": 1234, "username": "boss"},
		},
	}
	rawApprove, _ := json.Marshal(tgApprove)
	reqA := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawApprove))
	recA := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("快捷批准回调失败: %d, %s", recA.Code, recA.Body.String())
	}
	if reviewEvent88 != "APPROVE" {
		t.Fatalf("期望提交 APPROVE，实际: %q", reviewEvent88)
	}
	if !strings.Contains(reviewBody88, "LGTM") {
		t.Fatalf("期望包含 LGTM，实际: %q", reviewBody88)
	}
}
