package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
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
		{
			name:        "关联历史重复项并关闭",
			action:      "issue_mark_duplicate",
			issueNumber: 120,
			payload:     "10",
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

func TestChatOps_Feishu_ActorIDGuard(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 创建绑定了特定 ActorID (ou_bound_user) 的操作令牌
	tokenID, _ := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "issue_mark_spam_and_close", "repo-1", 123, "", "ou_bound_user", 10*time.Minute)

	// Case 1: 回调请求中未携带任何 open_id -> 必须严格拦截并提示无权，绝不放行
	emptyBody := map[string]any{
		"action": map[string]any{
			"value": map[string]any{
				"token": tokenID,
			},
		},
	}
	raw1, _ := json.Marshal(emptyBody)
	req1 := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(raw1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("期望响应 200 Toast, 实际: %d", w1.Code)
	}
	var res1 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &res1)
	toast1, _ := res1["toast"].(map[string]any)
	if toast1 == nil || toast1["type"] != "danger" || !strings.Contains(fmt.Sprint(toast1["content"]), "无权") {
		t.Fatalf("缺失身份时期望拦截并提示无权, 实际: %v", res1)
	}
	if exec.calls != 0 {
		t.Fatalf("缺失身份时不应触发执行器, 实际调用: %d", exec.calls)
	}

	// 令牌由于失败应被释放，重新用匹配的身份点击
	validBody := map[string]any{
		"open_id": "ou_bound_user",
		"action": map[string]any{
			"value": map[string]any{
				"token": tokenID,
			},
		},
	}
	raw2, _ := json.Marshal(validBody)
	req2 := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(raw2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("匹配身份时期望 200, 实际: %d", w2.Code)
	}
	if exec.calls != 1 {
		t.Fatalf("匹配身份时应成功执行, 实际调用: %d", exec.calls)
	}
}

func TestChatOps_FailClosed_CorruptedConfig(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 1. Telegram secret token 损坏时必须 500 失败关闭，绝不放行
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s1",
		Key:       "chatops.telegram.secret_token",
		ValueJSON: json.RawMessage("12345"),
	})
	tgReq := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader([]byte("{}")))
	tgReq.Header.Set("Content-Type", "application/json")
	tgW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(tgW, tgReq)
	if tgW.Code != http.StatusInternalServerError {
		t.Fatalf("Telegram 密钥损坏时期望 500 失败关闭, 实际: %d", tgW.Code)
	}

	// 2. 飞书 verification token 损坏时必须 500 失败关闭，绝不放行
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s2",
		Key:       "chatops.feishu.verification_token",
		ValueJSON: json.RawMessage("12345"),
	})
	feishuReq := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader([]byte("{}")))
	feishuReq.Header.Set("Content-Type", "application/json")
	feishuW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(feishuW, feishuReq)
	if feishuW.Code != http.StatusInternalServerError {
		t.Fatalf("飞书凭据损坏时期望 500 失败关闭, 实际: %d", feishuW.Code)
	}

	// 3. Telegram 白名单损坏时必须拒绝执行并报错
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s3",
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: json.RawMessage("12345"),
	})
	tokenID, _ := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "run-1", "", 10*time.Minute)
	tgBody := map[string]any{
		"callback_query": map[string]any{
			"id":   "cb_123",
			"data": "act:" + tokenID,
			"from": map[string]any{
				"id": 999999,
			},
		},
	}
	rawTg, _ := json.Marshal(tgBody)
	// 清理 secret token 免受第 1 步影响
	_ = fixture.store.Settings().Delete(t.Context(), "chatops.telegram.secret_token")
	tgReq2 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawTg))
	tgReq2.Header.Set("Content-Type", "application/json")
	tgW2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(tgW2, tgReq2)
	var tgRes2 map[string]any
	_ = json.Unmarshal(tgW2.Body.Bytes(), &tgRes2)
	if tgRes2["status"] != "error" {
		t.Fatalf("Telegram 白名单损坏时期望 status=error 失败关闭, 实际: %v", tgRes2)
	}
	if exec.calls != 0 {
		t.Fatalf("配置损坏时不应触发执行器, 实际调用: %d", exec.calls)
	}
}

func TestChatOps_Slack_Callback(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	signingSecret := "slack_secret_123456"
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s_slack_sec",
		Key:       "chatops.slack.signing_secret",
		ValueJSON: []byte(`"` + signingSecret + `"`),
	})
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s_slack_wl",
		Key:       "chatops.slack.allowed_user_ids",
		ValueJSON: []byte(`"U_VALID_USER"`),
	})

	tokenID, _ := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "workflow_rerun", "repo-1", 123, "", "U_VALID_USER", 10*time.Minute)

	payloadObj := map[string]any{
		"type": "block_actions",
		"user": map[string]any{
			"id": "U_VALID_USER",
		},
		"actions": []map[string]any{
			{
				"action_id": "workflow_rerun",
				"value":     tokenID,
			},
		},
	}
	rawPayload, _ := json.Marshal(payloadObj)
	formBody := "payload=" + url.QueryEscape(string(rawPayload))

	// Case 1: 缺少签名或时间戳超期 -> 401
	req1 := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader(formBody))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req1.Header.Set("X-Slack-Request-Timestamp", "1000") // 古老时间戳
	w1 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("超期时间戳期望 401, 实际: %d", w1.Code)
	}

	// Case 2: 签名错误 -> 401
	nowTs := strconv.FormatInt(time.Now().Unix(), 10)
	req2 := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader(formBody))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("X-Slack-Request-Timestamp", nowTs)
	req2.Header.Set("X-Slack-Signature", "v0=invalid_sig")
	w2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("错误签名期望 401, 实际: %d", w2.Code)
	}

	// 计算合法签名
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte("v0:" + nowTs + ":" + formBody))
	validSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Case 3: 合法签名与白名单用户 -> 200 成功执行
	req3 := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader(formBody))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.Header.Set("X-Slack-Request-Timestamp", nowTs)
	req3.Header.Set("X-Slack-Signature", validSig)
	w3 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("合法请求期望 200, 实际: %d", w3.Code)
	}
	if exec.calls != 1 {
		t.Fatalf("合法请求应调用执行器 1 次, 实际: %d", exec.calls)
	}
}

// failingSettingsStore 用于模拟 SettingsStore 发生瞬时数据库故障。
type failingSettingsStore struct {
	store.SettingsStore
	err error
}

func (f *failingSettingsStore) Get(ctx context.Context, key string) (store.SystemSetting, error) {
	if f.err != nil {
		return store.SystemSetting{}, f.err
	}
	return f.SettingsStore.Get(ctx, key)
}

type wrappedStore struct {
	store.Store
	customSettings store.SettingsStore
}

func (w *wrappedStore) Settings() store.SettingsStore {
	return w.customSettings
}

// TestChatOps_SecureConfig_Unit 单元测试：验证 readSecureConfig 在各种输入下的行为：
// 1. key 不存在 -> 返回空字符串与 nil（未配置）
// 2. 数据库瞬时错误 -> 返回错误
// 3. 空值 / null / 空白字符串 / 损坏类型 -> 一律返回错误（fail-closed）
// 4. 合法字符串 -> 返回有效值
func TestChatOps_SecureConfig_Unit(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	ctx := t.Context()

	// 1. 未配置（ErrNotFound）
	val, err := readSecureConfig(ctx, fixture.store.Settings(), "non_existent_key")
	if err != nil || val != "" {
		t.Fatalf("未配置时期望返回空字符串与 nil, 实际: val=%q err=%v", val, err)
	}

	// 2. 数据库瞬时故障
	failing := &failingSettingsStore{
		SettingsStore: fixture.store.Settings(),
		err:           errors.New("db connection timed out"),
	}
	_, err = readSecureConfig(ctx, failing, "any_key")
	if err == nil || !strings.Contains(err.Error(), "db connection timed out") {
		t.Fatalf("数据库瞬时错误时期望暴露错误, 实际: %v", err)
	}

	// 3. 空值三种场景：null、空字节、空串或纯空白
	cases := []struct {
		name      string
		rawJSON   string
		expectErr bool
	}{
		{name: "JSON null 字面量", rawJSON: "null", expectErr: true},
		{name: "JSON 空字符串", rawJSON: `""`, expectErr: true},
		{name: "JSON 纯空格字符串", rawJSON: `"   "`, expectErr: true},
		{name: "JSON 数字", rawJSON: "12345", expectErr: true},
		{name: "JSON 对象", rawJSON: `{"token": "abc"}`, expectErr: true},
		{name: "JSON 数组", rawJSON: `["abc"]`, expectErr: true},
		{name: "正常字符串配置", rawJSON: `"valid-secret-token"`, expectErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := "test.key." + tc.name
			_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
				ID:        k,
				Key:       k,
				ValueJSON: []byte(tc.rawJSON),
			})
			v, err := readSecureConfig(ctx, fixture.store.Settings(), k)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("期望返回配置异常错误, 实际为 nil (val=%q)", v)
				}
			} else {
				if err != nil || v != "valid-secret-token" {
					t.Fatalf("正常配置期望正确解析, 实际 val=%q err=%v", v, err)
				}
			}
		})
	}
}

// TestChatOps_FailClosed_NullAndDBError 针对三种输入（数据库瞬时错误、配置损坏、空值/null）
// 验证 Telegram/飞书/Slack 链路在回调时的 fail-closed 阻断保证。
func TestChatOps_FailClosed_NullAndDBError(t *testing.T) {
	exec := &recordingChatOpsExecutor{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 1. 测试 null 输入：Telegram secret_token 为 null 必须 500
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "null_tg_sec",
		Key:       "chatops.telegram.secret_token",
		ValueJSON: []byte("null"),
	})
	tgReq := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader([]byte("{}")))
	tgReq.Header.Set("Content-Type", "application/json")
	tgW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(tgW, tgReq)
	if tgW.Code != http.StatusInternalServerError {
		t.Fatalf("Telegram 密钥为 null 时期望 500 阻断, 实际: %d", tgW.Code)
	}

	// 2. 测试 null 输入：飞书 verification_token 为 null 必须 500
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "null_fs_tok",
		Key:       "chatops.feishu.verification_token",
		ValueJSON: []byte("null"),
	})
	fsReq := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader([]byte("{}")))
	fsReq.Header.Set("Content-Type", "application/json")
	fsW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(fsW, fsReq)
	if fsW.Code != http.StatusInternalServerError {
		t.Fatalf("飞书凭证为 null 时期望 500 阻断, 实际: %d", fsW.Code)
	}

	// 3. 测试 null 输入：Slack signing_secret 为 null 必须 500
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "null_slack_sec",
		Key:       "chatops.slack.signing_secret",
		ValueJSON: []byte("null"),
	})
	slackReq := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader("payload=%7B%7D"))
	slackReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	slackW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(slackW, slackReq)
	if slackW.Code != http.StatusInternalServerError {
		t.Fatalf("Slack 签名配置为 null 时期望 500 阻断, 实际: %d", slackW.Code)
	}

	// 4. 测试 null 输入：白名单为 null 时不可放行
	_ = fixture.store.Settings().Delete(t.Context(), "chatops.telegram.secret_token")
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "null_tg_wl",
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte("null"),
	})
	tokenID, _ := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "run-1", "", 10*time.Minute)
	tgBody := map[string]any{
		"callback_query": map[string]any{
			"id":   "cb_null_test",
			"data": "act:" + tokenID,
			"from": map[string]any{
				"id": 888888,
			},
		},
	}
	rawTg, _ := json.Marshal(tgBody)
	tgReq2 := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawTg))
	tgReq2.Header.Set("Content-Type", "application/json")
	tgW2 := httptest.NewRecorder()
	fixture.handler.ServeHTTP(tgW2, tgReq2)
	var res2 map[string]any
	_ = json.Unmarshal(tgW2.Body.Bytes(), &res2)
	if res2["status"] != "error" {
		t.Fatalf("Telegram 白名单为 null 时必须报错阻断, 实际响应: %v", res2)
	}
	if exec.calls != 0 {
		t.Fatalf("白名单为 null 时绝不应调用执行器, 实际调用: %d", exec.calls)
	}
}

// TestChatOps_ExecutorErrorSanitization 验证 Telegram 和 Slack 回调发生执行器错误时，
// 绝不将内部执行错误细节透给外部调用端。
func TestChatOps_ExecutorErrorSanitization(t *testing.T) {
	sensitiveErrMsg := "internal connection pool exhausted to postgres://admin:pass@10.0.0.8:5432"
	exec := &recordingChatOpsExecutor{
		err: errors.New(sensitiveErrMsg),
	}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		chatOpsExecutor: exec,
	})

	// 1. Telegram 回调报错脱敏
	tokenID, _ := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "run-2", "", 10*time.Minute)
	tgBody := map[string]any{
		"callback_query": map[string]any{
			"id":   "cb_sanitize_test",
			"data": "act:" + tokenID,
			"from": map[string]any{
				"id": 123456,
			},
		},
	}
	rawTg, _ := json.Marshal(tgBody)
	tgReq := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawTg))
	tgReq.Header.Set("Content-Type", "application/json")
	tgW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(tgW, tgReq)
	var tgRes map[string]any
	_ = json.Unmarshal(tgW.Body.Bytes(), &tgRes)

	if tgRes["status"] != "action_failed" {
		t.Fatalf("期望 status=action_failed, 实际: %v", tgRes)
	}
	// 验证错误信息已脱敏为通用常量，未泄漏敏感信息
	if tgRes["error"] != "action_execution_failed" {
		t.Fatalf("期望 error=action_execution_failed, 实际: %v", tgRes["error"])
	}
	if strings.Contains(tgW.Body.String(), "postgres") || strings.Contains(tgW.Body.String(), "10.0.0.8") {
		t.Fatalf("Telegram 回调响应泄漏了内部敏感信息: %s", tgW.Body.String())
	}

	// 2. Slack 回调报错脱敏
	slackTokenID, _ := CreateChatOpsTokenWithPayload(t.Context(), fixture.store, "workflow_rerun", "repo-1", 123, "", "U_SLACK_USER", 10*time.Minute)
	payloadObj := map[string]any{
		"type": "block_actions",
		"user": map[string]any{
			"id": "U_SLACK_USER",
		},
		"actions": []map[string]any{
			{
				"action_id": "workflow_rerun",
				"value":     slackTokenID,
			},
		},
	}
	rawPayload, _ := json.Marshal(payloadObj)
	formBody := "payload=" + url.QueryEscape(string(rawPayload))

	slackReq := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader(formBody))
	slackReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	slackW := httptest.NewRecorder()
	fixture.handler.ServeHTTP(slackW, slackReq)

	var slackRes map[string]any
	_ = json.Unmarshal(slackW.Body.Bytes(), &slackRes)
	text, _ := slackRes["text"].(string)
	if !strings.Contains(text, "操作执行失败，请稍后重试") {
		t.Fatalf("期望通用提示语, 实际: %s", text)
	}
	if strings.Contains(slackW.Body.String(), "postgres") || strings.Contains(slackW.Body.String(), "10.0.0.8") {
		t.Fatalf("Slack 回调响应泄漏了内部敏感信息: %s", slackW.Body.String())
	}
}

type mockHTTPChatOpsJevClient struct {
	judgeFunc func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
}

func (m *mockHTTPChatOpsJevClient) Judge(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
	if m.judgeFunc != nil {
		return m.judgeFunc(ctx, state, questions)
	}
	return jev.Result{}, nil
}

func httpPtrFloat(f float64) *float64 { return &f }
func httpPtrString(s string) *string  { return &s }

func TestChatOps_HTTP_GuardrailFeedback(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name        string
		judgeFunc   func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
		wantStatus  string
		wantToast   string
		wantMessage string
	}{
		{
			name: "denied",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: httpPtrString("deny"), Confidence: httpPtrFloat(0.95)},
					},
				}, nil
			},
			wantStatus:  "guardrail_denied",
			wantToast:   "danger",
			wantMessage: "高风险操作已被安全护栏拒绝",
		},
		{
			name: "unavailable",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{}, errors.New("timeout")
			},
			wantStatus:  "guardrail_unavailable",
			wantToast:   "warning",
			wantMessage: "安全护栏分析未通过或超时",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jevCfg := config.JevConfig{
				Enabled:                 true,
				ChatOpsGuardrailEnabled: true,
				Mode:                    "active",
			}
			rt := jev.RuntimeFromEnv(jevCfg)
			rt.SetClientForTest("chatops", &mockHTTPChatOpsJevClient{judgeFunc: tc.judgeFunc})

			exec := &recordingChatOpsExecutor{}
			fixture := newHTTPTestFixture(t, httpTestOptions{
				chatOpsExecutor: exec,
				jevRuntime:      rt,
			})

			// 1. Telegram
			_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
				ID:        ulid.Make().String(),
				Key:       "chatops.telegram.allowed_user_ids",
				ValueJSON: []byte(`"12345"`),
			})
			tgToken, _ := CreateChatOpsToken(ctx, fixture.store, "issue_reply_draft", "repo-1", "issue-1", "12345", 10*time.Minute)

			tgBody := map[string]any{
				"callback_query": map[string]any{
					"id":   "cb-guardrail-test",
					"data": "act:" + tgToken,
					"from": map[string]any{
						"id": 12345,
					},
				},
			}
			rawTg, _ := json.Marshal(tgBody)
			tgReq := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(rawTg))
			tgReq.Header.Set("Content-Type", "application/json")
			tgW := httptest.NewRecorder()
			fixture.handler.ServeHTTP(tgW, tgReq)

			if tgW.Code != http.StatusOK {
				t.Fatalf("Telegram HTTP status expected 200, got %d", tgW.Code)
			}
			var tgRes map[string]any
			_ = json.Unmarshal(tgW.Body.Bytes(), &tgRes)
			if tgRes["status"] != tc.wantStatus {
				t.Fatalf("Telegram expected status %s, got %v", tc.wantStatus, tgRes)
			}

			// 2. Feishu
			_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
				ID:        ulid.Make().String(),
				Key:       "chatops.feishu.allowed_user_ids",
				ValueJSON: []byte(`"ou_user_1"`),
			})
			fsToken, _ := CreateChatOpsToken(ctx, fixture.store, "issue_reply_draft", "repo-1", "issue-1", "ou_user_1", 10*time.Minute)
			fsBody := map[string]any{
				"open_id": "ou_user_1",
				"action": map[string]any{
					"value": map[string]any{
						"token": fsToken,
					},
				},
			}
			rawFs, _ := json.Marshal(fsBody)
			fsReq := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(rawFs))
			fsReq.Header.Set("Content-Type", "application/json")
			fsW := httptest.NewRecorder()
			fixture.handler.ServeHTTP(fsW, fsReq)

			if fsW.Code != http.StatusOK {
				t.Fatalf("Feishu HTTP status expected 200, got %d", fsW.Code)
			}
			var fsRes struct {
				Toast struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				} `json:"toast"`
			}
			_ = json.Unmarshal(fsW.Body.Bytes(), &fsRes)
			if fsRes.Toast.Type != tc.wantToast {
				t.Fatalf("Feishu expected toast type %s, got %s (content: %s)", tc.wantToast, fsRes.Toast.Type, fsRes.Toast.Content)
			}

			// 3. Slack
			_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
				ID:        ulid.Make().String(),
				Key:       "chatops.slack.allowed_user_ids",
				ValueJSON: []byte(`"U12345"`),
			})
			slToken, _ := CreateChatOpsToken(ctx, fixture.store, "issue_reply_draft", "repo-1", "issue-1", "U12345", 10*time.Minute)
			slPayload := map[string]any{
				"user": map[string]any{
					"id": "U12345",
				},
				"actions": []map[string]any{
					{"value": slToken},
				},
			}
			rawPayload, _ := json.Marshal(slPayload)
			formBody := "payload=" + url.QueryEscape(string(rawPayload))
			slReq := httptest.NewRequest(http.MethodPost, "/chatops/slack/callback", strings.NewReader(formBody))
			slReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			slW := httptest.NewRecorder()
			fixture.handler.ServeHTTP(slW, slReq)

			if slW.Code != http.StatusOK {
				t.Fatalf("Slack HTTP status expected 200, got %d", slW.Code)
			}
			var slRes struct {
				ResponseType string `json:"response_type"`
				Text         string `json:"text"`
			}
			_ = json.Unmarshal(slW.Body.Bytes(), &slRes)
			if slRes.ResponseType != "ephemeral" {
				t.Fatalf("Slack expected response_type ephemeral, got %s", slRes.ResponseType)
			}
			if !strings.Contains(slRes.Text, tc.wantMessage) {
				t.Fatalf("Slack expected text to contain %q, got %q", tc.wantMessage, slRes.Text)
			}

			// Executor 绝不可调用
			if exec.calls != 0 {
				t.Fatalf("Executor should not be called when guardrail blocks, got %d calls", exec.calls)
			}
		})
	}
}

func TestChatOpsWebAction_RequiresAdminAuthAndExecutes(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	unauth := fixture.request(t, http.MethodPost, "/api/v1/chatops/web-action", `{}`, "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	body := `{"action":"invalid_action","repo_id":"test-repo-id"}`
	resp := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		body, "127.0.0.1:45002", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if resp.Code == http.StatusNotFound {
		t.Fatalf("expected route /api/v1/chatops/web-action to exist, got 404")
	}
}

type mockWebChatOpsExecutor struct {
	lastAction chatops.ActionData
	outcome    *chatops.Outcome
	err        error
}

func (m *mockWebChatOpsExecutor) Execute(ctx context.Context, data chatops.ActionData) error {
	m.lastAction = data
	return m.err
}

func (m *mockWebChatOpsExecutor) ExecuteWithOutcome(ctx context.Context, data chatops.ActionData) (*chatops.Outcome, error) {
	m.lastAction = data
	return m.outcome, m.err
}

func TestChatOpsWebAction_ExecuteSuccess(t *testing.T) {
	mockExec := &mockWebChatOpsExecutor{
		outcome: &chatops.Outcome{
			Status:  chatops.StatusExecuted,
			Action:  "issue_reply_draft",
			Message: "已成功回复草稿",
		},
	}
	fixture := newHTTPTestFixture(t, httpTestOptions{chatOpsExecutor: mockExec})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	_, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             "repo-1",
		Owner:          "org",
		Name:           "repo-1",
		FullName:       "org/repo-1",
		DefaultBranch:  "main",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	body := `{"action":"issue_reply_draft","repo_id":"repo-1","issue_number":42,"payload":"thanks for the PR"}`
	resp := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		body, "127.0.0.1:45003", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.Code, resp.Body.String())
	}
	if mockExec.lastAction.Action != "issue_reply_draft" || mockExec.lastAction.IssueNumber != 42 {
		t.Errorf("unexpected action data executed: %+v", mockExec.lastAction)
	}
}

func TestChatOpsWebAction_SecurityAndValidation(t *testing.T) {
	mockExec := &mockWebChatOpsExecutor{
		outcome: &chatops.Outcome{
			Status:  chatops.StatusExecuted,
			Action:  "workflow_rerun",
			Message: "ok",
		},
	}
	fixture := newHTTPTestFixture(t, httpTestOptions{chatOpsExecutor: mockExec})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	_, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             "monitored-repo",
		Owner:          "acme",
		Name:           "monitored-repo",
		FullName:       "acme/monitored-repo",
		DefaultBranch:  "main",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	_, _ = fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:            "unmonitored-repo",
		Owner:         "acme",
		Name:          "unmonitored-repo",
		FullName:      "acme/unmonitored-repo",
		DefaultBranch: "main",
		IsArchived:    true,
	})

	// 1. 未登录访问 -> 401 Unauthorized
	unauth := fixture.request(t, http.MethodPost, "/api/v1/chatops/web-action", `{"action":"workflow_rerun","repo_id":"monitored-repo"}`, "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 2. 未知 Action -> 400 ValidationFailed
	badAction := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		`{"action":"unknown_destructive_action","repo_id":"monitored-repo"}`, "127.0.0.1:45002", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	assertAPIError(t, badAction, http.StatusBadRequest, errorCodeValidationFailed)

	// 3. 不存在的 RepoID -> 404 NotFound
	missingRepo := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		`{"action":"workflow_rerun","repo_id":"no-such-repo"}`, "127.0.0.1:45003", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	assertAPIError(t, missingRepo, http.StatusNotFound, errorCodeNotFound)

	// 4. 未监控的 RepoID -> 400 ValidationFailed
	unmonitored := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		`{"action":"workflow_rerun","repo_id":"unmonitored-repo"}`, "127.0.0.1:45004", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	assertAPIError(t, unmonitored, http.StatusBadRequest, errorCodeValidationFailed)

	// 5. 带有 IdempotencyKey -> 校验传递至 ActionData.ID
	idempotentKey := "custom-idempotency-key-12345"
	resp := fixture.request(
		t, http.MethodPost, "/api/v1/chatops/web-action",
		fmt.Sprintf(`{"action":"workflow_rerun","repo_id":"monitored-repo","idempotency_key":"%s"}`, idempotentKey), "127.0.0.1:45005", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.Code, resp.Body.String())
	}
	if mockExec.lastAction.ID != idempotentKey {
		t.Errorf("expected ActionData.ID to match idempotency key %s, got %s", idempotentKey, mockExec.lastAction.ID)
	}
}
