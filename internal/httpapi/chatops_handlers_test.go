package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

type recordingChatOpsExecutor struct {
	action ChatOpsActionData
	calls  int
	err    error
}

func (e *recordingChatOpsExecutor) Execute(_ context.Context, action ChatOpsActionData) error {
	e.action = action
	e.calls++
	return e.err
}

func TestChatOps_Telegram_SecretTokenVerification(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Configure telegram secret token in settings
	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.telegram.secret_token",
		ValueJSON: json.RawMessage(`"my-secret-token-123"`),
	})
	if err != nil {
		t.Fatalf("failed to seed telegram secret token: %v", err)
	}

	payload := []byte(`{"update_id":123,"callback_query":{"id":"cb-1","data":"act:test-token"}}`)

	// Case 1: Missing header -> 403 Forbidden
	req := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on missing secret token, got %d: %s", w.Code, w.Body.String())
	}

	// Case 2: Wrong header -> 403 Forbidden
	req = httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on wrong secret token, got %d: %s", w.Code, w.Body.String())
	}

	// Case 3: Correct header -> Accepted (200 OK)
	req = httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret-token-123")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on correct secret token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatOps_Feishu_URLVerificationAndSignature(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Configure feishu verification token in settings
	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.feishu.verification_token",
		ValueJSON: json.RawMessage(`"feishu-token-xyz"`),
	})
	if err != nil {
		t.Fatalf("failed to seed feishu token: %v", err)
	}

	// Case 1: URL verification challenge
	challengePayload := []byte(`{"type":"url_verification","challenge":"challenge_code_999","token":"feishu-token-xyz"}`)
	req := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(challengePayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on challenge, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["challenge"] != "challenge_code_999" {
		t.Fatalf("expected challenge 'challenge_code_999', got %v", res)
	}

	// Case 2: URL challenge with wrong token -> 401 Unauthorized
	wrongChallenge := []byte(`{"type":"url_verification","challenge":"abc","token":"wrong"}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(wrongChallenge))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong challenge token, got %d", w.Code)
	}

	// Case 3: Interactive card action with wrong token -> 401 Unauthorized
	wrongCard := []byte(`{"type":"interactive","token":"wrong","action":{"value":{"token":"tok123"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(wrongCard))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong interactive card token, got %d", w.Code)
	}

	// Case 4: Interactive card action with correct token -> 200 OK
	validCard := []byte(`{"type":"interactive","token":"feishu-token-xyz","action":{"value":{"token":"tok123"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(validCard))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid interactive card, got %d", w.Code)
	}
}

func TestChatOps_ActionToken_AntiReplay(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Register a single-use action token
	tokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "123456", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to create chatops token: %v", err)
	}

	// First consumption should succeed
	actionData, err := ConsumeChatOpsToken(t.Context(), fixture.store, tokenID)
	if err != nil {
		t.Fatalf("first token consumption should succeed, got: %v", err)
	}
	if actionData.Action != "workflow_rerun" || actionData.RepoID != "repo-1" {
		t.Fatalf("unexpected action data: %+v", actionData)
	}

	// Second consumption should fail immediately (consumed / replay rejected)
	_, err = ConsumeChatOpsToken(t.Context(), fixture.store, tokenID)
	if err == nil {
		t.Fatal("expected second consumption to fail with replay error, got nil")
	}
}

func TestChatOps_ActionToken_ConcurrentConsumptionAllowsOnlyOneWinner(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	tokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "123456", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := ConsumeChatOpsToken(t.Context(), fixture.store, tokenID)
			results <- err
		}()
	}

	winners := 0
	for i := 0; i < 8; i++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one token consumer, got %d", winners)
	}
}

func TestChatOps_ActionToken_ReleaseAllowsRetry(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	tokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "123456", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeChatOpsToken(t.Context(), fixture.store, tokenID); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseChatOpsToken(t.Context(), fixture.store, tokenID); err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeChatOpsToken(t.Context(), fixture.store, tokenID); err != nil {
		t.Fatalf("released token should be retryable: %v", err)
	}
}

func TestChatOps_IssueActions_CallbackDispatch(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	tests := []struct {
		name        string
		action      string
		issueNumber int
		payload     string
	}{
		{
			name:        "采纳建议首响应草稿",
			action:      "issue_reply_draft",
			issueNumber: 42,
			payload:     "你好，感谢反馈，请问是否有完整复现步骤？",
		},
		{
			name:        "标记垃圾并关闭Issue",
			action:      "issue_mark_spam_and_close",
			issueNumber: 99,
			payload:     "",
		},
		{
			name:        "应用建议标签",
			action:      "issue_apply_labels",
			issueNumber: 101,
			payload:     "bug,triage",
		},
		{
			name:        "索取复现要素",
			action:      "issue_request_details",
			issueNumber: 77,
			payload:     "请提供复现步骤与日志",
		},
		{
			name:        "延迟提醒",
			action:      "issue_snooze",
			issueNumber: 88,
			payload:     "acme/repo|88|https://github.com/acme/repo/issues/88",
		},
		{
			name:        "快捷批准PR",
			action:      "pr_quick_approve",
			issueNumber: 99,
			payload:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tokenID, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, tc.action, "repo-1", tc.issueNumber, tc.payload, "", 10*time.Minute)
			if err != nil {
				t.Fatalf("创建令牌失败: %v", err)
			}

			tgBody := map[string]any{
				"update_id": 1,
				"callback_query": map[string]any{
					"id":   "cb-" + tc.action,
					"data": "act:" + tokenID,
					"from": map[string]any{
						"id":       123456,
						"username": "maintainer",
					},
				},
			}
			raw, _ := json.Marshal(tgBody)
			req := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("期望状态码 200, 实际: %d, body: %s", w.Code, w.Body.String())
			}
			if exec.action.Action != tc.action {
				t.Fatalf("期望执行动作 %s, 实际: %s", tc.action, exec.action.Action)
			}
			if exec.action.IssueNumber != tc.issueNumber {
				t.Fatalf("期望Issue编号 %d, 实际: %d", tc.issueNumber, exec.action.IssueNumber)
			}
			if exec.action.Payload != tc.payload {
				t.Fatalf("期望载荷 %q, 实际: %q", tc.payload, exec.action.Payload)
			}
		})
	}
}

func TestChatOps_Telegram_EditMessageReplyMarkup(t *testing.T) {
	var receivedEditBody map[string]any
	editCalled := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/editMessageReplyMarkup" {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedEditBody)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			select {
			case editCalled <- struct{}{}:
			default:
			}
			return
		}
		if r.URL.Path == "/answerCallbackQuery" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 配置 bot token 为 mock server URL
	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.telegram.bot_token",
		ValueJSON: json.RawMessage(fmt.Sprintf("%q", srv.URL)),
	})
	if err != nil {
		t.Fatalf("配置 bot_token 失败: %v", err)
	}

	tokenID, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_reply_draft", "repo-1", 42, "draft", "", 10*time.Minute)
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	tgBody := map[string]any{
		"update_id": 1,
		"callback_query": map[string]any{
			"id":   "cb-1",
			"data": "act:" + tokenID,
			"from": map[string]any{"id": 123456, "username": "boss"},
			"message": map[string]any{
				"message_id": 999,
				"chat":       map[string]any{"id": -100123},
				"reply_markup": map[string]any{
					"inline_keyboard": [][]map[string]any{
						{
							{"text": "🔗 前往 GitHub", "url": "https://github.com/o/r/issues/42"},
						},
						{
							{"text": "💬 采纳建议一键回复", "callback_data": "act:" + tokenID},
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(tgBody)
	req := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际: %d", w.Code)
	}

	select {
	case <-editCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("超时未收到 editMessageReplyMarkup 调用")
	}

	if chatID, _ := receivedEditBody["chat_id"].(float64); int64(chatID) != -100123 {
		t.Fatalf("chat_id 期望 -100123, 实际: %v", receivedEditBody["chat_id"])
	}
	if msgID, _ := receivedEditBody["message_id"].(float64); int(msgID) != 999 {
		t.Fatalf("message_id 期望 999, 实际: %v", receivedEditBody["message_id"])
	}
	rm, ok := receivedEditBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("期望 reply_markup 存在: %v", receivedEditBody)
	}
	kb, ok := rm["inline_keyboard"].([]any)
	if !ok || len(kb) != 2 {
		t.Fatalf("期望 inline_keyboard 有 2 行: %v", kb)
	}
	row1 := kb[1].([]any)
	btn1 := row1[0].(map[string]any)
	if btn1["text"] != "✅ 已采纳回复" {
		t.Fatalf("期望按钮文案变为 ✅ 已采纳回复, 实际: %v", btn1["text"])
	}
	if btn1["callback_data"] != "done:issue_reply_draft" {
		t.Fatalf("期望 callback_data 变为 done:issue_reply_draft, 实际: %v", btn1["callback_data"])
	}
}

func TestChatOps_Telegram_AllowedUserIDsGuard(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 设置仅允许维护者 88888 操作
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: json.RawMessage(`"88888,99999"`),
	})

	tokenID, _ := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_spam_and_close", "repo-1", 123, "", "", 10*time.Minute)

	// Case 1: 未授权用户 11111 点击 -> 403 Forbidden（直接提前拦截，无需且未消费令牌）
	tgUnauthorized := map[string]any{
		"update_id": 1,
		"callback_query": map[string]any{
			"id":   "cb-unauth",
			"data": "act:" + tokenID,
			"from": map[string]any{"id": 11111, "username": "stranger"},
		},
	}
	raw1, _ := json.Marshal(tgUnauthorized)
	req1 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(raw1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("未授权用户回调应响应 200 避免 Telegram 平台重试, 实际: %d", w1.Code)
	}
	var res1 map[string]any
	if err := json.Unmarshal(w1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if res1["status"] != "forbidden" || res1["error"] != "telegram_user_not_allowed" {
		t.Fatalf("期望状态 forbidden / telegram_user_not_allowed, 实际: %v", res1)
	}
	if exec.calls != 0 {
		t.Fatalf("未授权用户不应触发执行器，实际调用次数: %d", exec.calls)
	}

	// Case 2: 授权用户 88888 点击 -> 200 OK，正常消费未被未授权用户污染的令牌
	tgAuthorized := map[string]any{
		"update_id": 2,
		"callback_query": map[string]any{
			"id":   "cb-auth",
			"data": "act:" + tokenID,
			"from": map[string]any{"id": 88888, "username": "maintainer"},
		},
	}
	raw2, _ := json.Marshal(tgAuthorized)
	req2 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(raw2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("授权用户期望 200, 实际: %d", w2.Code)
	}
	if exec.calls != 1 {
		t.Fatalf("授权用户应成功触发执行器，实际调用次数: %d", exec.calls)
	}
}

func TestGitHubChatOpsExecutor_CompositeActionHandling(t *testing.T) {
	var labelCalls int
	var closeCalls int

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == http.MethodPost:
			labelCalls++
			if strings.Contains(r.URL.Path, "/issues/500/labels") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if strings.Contains(r.URL.Path, "/issues/501/labels") {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"Label does not exist"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"invalid"}]`))
		case strings.HasSuffix(r.URL.Path, "/issues/100") && r.Method == http.MethodPatch:
			closeCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"state":"closed"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer mockServer.Close()

	ghClient := githubx.NewAppClient(1, "")
	ghClient.BaseURL = mockServer.URL
	ghClient.HTTP = mockServer.Client()

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})

	repo, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             ulid.Make().String(),
		Owner:          "acme",
		Name:           "repo-composite",
		FullName:       "acme/repo-composite",
		DefaultBranch:  "main",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	exec := &GitHubChatOpsExecutor{
		Store:  fixture.store,
		Client: ghClient,
	}

	// 1. 测试 issue_mark_spam_and_close 正常闭环
	err = exec.Execute(t.Context(), ChatOpsActionData{
		ID:          "act-spam-1",
		Action:      "issue_mark_spam_and_close",
		RepoID:      repo.ID,
		IssueNumber: 100,
	})
	if err != nil {
		t.Fatalf("issue_mark_spam_and_close 期望执行成功，实际: %v", err)
	}
	if labelCalls != 1 || closeCalls != 1 {
		t.Fatalf("期望打标与关闭均被调用，实际 labelCalls: %d, closeCalls: %d", labelCalls, closeCalls)
	}

	// 2. 测试 issue_snooze 正常写入 Outbox 并正确转义 link
	_, err = fixture.store.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID:          ulid.Make().String(),
		ChannelType: store.ChannelTelegram,
		Target:      "chat-snooze",
		Enabled:     true,
		EventKinds:  []string{store.WorkItemKindIssue},
	})
	if err != nil {
		t.Fatalf("创建通道失败: %v", err)
	}

	err = exec.Execute(t.Context(), ChatOpsActionData{
		ID:          "act-snooze-1",
		Action:      "issue_snooze",
		RepoID:      repo.ID,
		IssueNumber: 100,
		Payload:     "acme/repo-composite|Issue Title|https://github.com/acme/repo-composite/issues/100?a=1&b=2",
	})
	if err != nil {
		t.Fatalf("issue_snooze 期望成功，实际: %v", err)
	}

	outboxList, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{PerPage: 10})
	if err != nil || len(outboxList) == 0 {
		t.Fatalf("期望在 Outbox 生成待办提醒，实际: %v, count: %d", err, len(outboxList))
	}
	foundSnooze := false
	for _, o := range outboxList {
		if strings.Contains(o.Title, "[待办提醒]") {
			foundSnooze = true
			if !strings.Contains(o.BodyText, "https://github.com/acme/repo-composite/issues/100?a=1&amp;b=2") {
				t.Fatalf("HTML 链接应正确进行转义，实际正文: %s", o.BodyText)
			}
		}
	}
	if !foundSnooze {
		t.Fatalf("未找到生成的待办提醒消息")
	}

	// 3. 测试打标失败时正确暴露错误，不静默丢弃
	err = exec.Execute(t.Context(), ChatOpsActionData{
		ID:          "act-spam-err",
		Action:      "issue_mark_spam_and_close",
		RepoID:      repo.ID,
		IssueNumber: 500,
	})
	if err == nil || !strings.Contains(err.Error(), "add spam label") {
		t.Fatalf("期望暴露打标失败错误，实际: %v", err)
	}

	// 4. 测试打标遭遇 422 标签不存在时，容错成功关闭 Issue
	err = exec.Execute(t.Context(), ChatOpsActionData{
		ID:          "act-spam-422",
		Action:      "issue_mark_spam_and_close",
		RepoID:      repo.ID,
		IssueNumber: 501,
	})
	if err != nil {
		t.Fatalf("打标遇 422 标签不存在时期望容错完成关闭，实际报错: %v", err)
	}
}

func TestChatOps_Feishu_AllowedUserIDsGuard(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 配置飞书白名单
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.feishu.allowed_user_ids",
		ValueJSON: json.RawMessage(`"ou_maintainer1,ou_maintainer2"`),
	})

	tokenID, _ := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_spam_and_close", "repo-1", 123, "", "", 10*time.Minute)

	// Case 1: 未授权用户 ou_stranger 点击 -> 返回 200 并携带警告 Toast，拦截执行
	unauthBody := map[string]any{
		"open_id": "ou_stranger",
		"action": map[string]any{
			"value": map[string]any{
				"token": tokenID,
			},
		},
	}
	raw1, _ := json.Marshal(unauthBody)
	req1 := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(raw1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("未授权请求期望返回 200 Toast, 实际: %d", w1.Code)
	}
	var res1 map[string]any
	if err := json.Unmarshal(w1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	toast1, ok := res1["toast"].(map[string]any)
	if !ok || toast1["type"] != "warning" || !strings.Contains(fmt.Sprint(toast1["content"]), "受信任") {
		t.Fatalf("期望返回拦截 Toast, 实际: %v", res1)
	}
	if exec.calls != 0 {
		t.Fatalf("未授权用户不应触发执行器，调用次数: %d", exec.calls)
	}

	// Case 2: 授权用户 ou_maintainer1 点击 -> 返回 200 且正常执行
	authBody := map[string]any{
		"open_id": "ou_maintainer1",
		"action": map[string]any{
			"value": map[string]any{
				"token": tokenID,
			},
		},
	}
	raw2, _ := json.Marshal(authBody)
	req2 := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(raw2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("授权请求期望返回 200, 实际: %d", w2.Code)
	}
	var res2 map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	toast2, ok := res2["toast"].(map[string]any)
	if !ok || toast2["type"] != "success" {
		t.Fatalf("期望返回成功 Toast, 实际: %v", res2)
	}
	if exec.calls != 1 {
		t.Fatalf("授权用户应成功触发执行器，调用次数: %d", exec.calls)
	}
}

func TestChatOps_Telegram_EditMessageReplyMarkup_SpamCollapse(t *testing.T) {
	var receivedEditBody map[string]any
	editCalled := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/editMessageReplyMarkup" {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedEditBody)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			select {
			case editCalled <- struct{}{}:
			default:
			}
			return
		}
		if r.URL.Path == "/answerCallbackQuery" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.telegram.bot_token",
		ValueJSON: json.RawMessage(fmt.Sprintf("%q", srv.URL)),
	})
	if err != nil {
		t.Fatalf("配置 bot_token 失败: %v", err)
	}

	tokenID, err := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_spam_and_close", "repo-1", 42, "", "", 10*time.Minute)
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	tgBody := map[string]any{
		"update_id": 1,
		"callback_query": map[string]any{
			"id":   "cb-spam",
			"data": "act:" + tokenID,
			"from": map[string]any{"id": 123456, "username": "maintainer"},
			"message": map[string]any{
				"message_id": 1001,
				"chat":       map[string]any{"id": -100456},
				"reply_markup": map[string]any{
					"inline_keyboard": [][]map[string]any{
						{
							{"text": "🔗 前往 GitHub", "url": "https://github.com/o/r/issues/42"},
						},
						{
							{"text": "🚫 标记垃圾并关闭", "callback_data": "act:" + tokenID},
						},
						{
							{"text": "💬 采纳建议一键回复", "callback_data": "act:other-tok"},
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(tgBody)
	req := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际: %d", w.Code)
	}

	select {
	case <-editCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("超时未收到 editMessageReplyMarkup 调用")
	}

	rm, ok := receivedEditBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("期望 reply_markup 存在: %v", receivedEditBody)
	}
	kb, ok := rm["inline_keyboard"].([]any)
	if !ok || len(kb) != 1 {
		t.Fatalf("垃圾标记后键盘应收敛为 1 行单一按钮, 实际行数: %d", len(kb))
	}
	row0 := kb[0].([]any)
	if len(row0) != 1 {
		t.Fatalf("期望单行只有 1 个按钮, 实际: %d", len(row0))
	}
	btn0 := row0[0].(map[string]any)
	if btn0["text"] != "🚫 已标记关闭" {
		t.Fatalf("期望按钮文案变为 🚫 已标记关闭, 实际: %v", btn0["text"])
	}
	if btn0["callback_data"] != "done:issue_mark_spam_and_close" {
		t.Fatalf("期望 callback_data 变为 done:issue_mark_spam_and_close, 实际: %v", btn0["callback_data"])
	}
}
