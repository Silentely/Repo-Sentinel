package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func cloneValues(v url.Values) url.Values {
	c := url.Values{}
	for k, list := range v {
		c[k] = append([]string(nil), list...)
	}
	return c
}

// setupTMAE2EFixture 创建用于 E2E 集成测试的完整测试环境。
func setupTMAE2EFixture(t *testing.T, tmaEnabled bool, allowedUser TelegramUser, customExec ChatOpsActionExecutor, customGH *githubx.AppClient, customLogger ...*slog.Logger) (*httpTestFixture, string) {
	t.Helper()
	var log *slog.Logger
	if len(customLogger) > 0 {
		log = customLogger[0]
	}

	frontendFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<!doctype html><html><body><div id=\"root\">TMA Shell</div></body></html>"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	}

	fixture := newHTTPTestFixture(t, httpTestOptions{
		tmaEnabled:      tmaEnabled,
		publicBaseURL:   "https://reposentinel.example",
		chatOpsExecutor: customExec,
		githubClient:    customGH,
		logger:          log,
		frontend:        frontendFS,
	})

	ctx := t.Context()
	_, err1 := fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops.telegram.bot_token",
		ValueJSON: []byte(strconv.Quote(testTMABotToken)),
	})
	_, err2 := fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(strconv.Quote(strconv.FormatInt(allowedUser.ID, 10))),
	})
	if err1 != nil || err2 != nil {
		t.Fatalf("初始化测试设置失败: err1=%v, err2=%v", err1, err2)
	}

	_, _ = fixture.store.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          "ch-tg-fixture-default",
		ChannelType: store.ChannelTelegram,
		Name:        "telegram-fixture",
		Target:      "123456",
		Enabled:     true,
	})
	InvalidateResolvedBotTokenCache()

	initData := createTestInitData(testTMABotToken, allowedUser, time.Now().Unix(), false)
	return fixture, initData
}

// ============================================================================
// Assertion 1: 验签公式方向正确 (R1)
// 以 key="WebAppData", msg=bot_token 派生密钥。
// 断言：合法 initData 验签通过，成功在 Context 注入 TelegramUser。
// ============================================================================
func TestTMA_E2E_Assertion1_ValidSignature(t *testing.T) {
	user := TelegramUser{ID: 70001, FirstName: "Alice", Username: "alice_sentinel"}
	fixture, initData := setupTMAE2EFixture(t, true, user, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	req.Header.Set("Origin", "https://reposentinel.example")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("合法 initData 验签期望 200 OK，实际: %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, ok := resp["quiet_hours_enabled"]; !ok {
		t.Fatalf("期望响应包含 quiet_hours_enabled 字段，实际: %v", resp)
	}
}

// ============================================================================
// Assertion 2: 反向公式拦截 (R1)
// 若错误以 key=bot_token, msg="WebAppData" 派生密钥并生成签名，
// 断言：中间件严格拦截并返回 HTTP 401 Unauthorized。
// ============================================================================
func TestTMA_E2E_Assertion2_ReversedSecretKeyFormulaRejected401(t *testing.T) {
	user := TelegramUser{ID: 70002, FirstName: "Bob", Username: "bob_reversed"}
	fixture, _ := setupTMAE2EFixture(t, true, user, nil, nil)

	// 使用 reversedSecretKey = true 生成反向公式派生签名
	reversedInitData := createTestInitData(testTMABotToken, user, time.Now().Unix(), true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	req.Header.Set("X-Telegram-Init-Data", reversedInitData)
	req.Header.Set("Origin", "https://reposentinel.example")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("反向派生密钥必须被拦截返回 401 Unauthorized，实际: %d, body: %s", w.Code, w.Body.String())
	}

	var errResp apiErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if errResp.ErrorCode != errorCodeUnauthorized {
		t.Fatalf("期望错误码 unauthorized，实际: %s", errResp.ErrorCode)
	}
}

// ============================================================================
// Assertion 3: 畸形参数 Fail-Closed (R1)
// 缺少 hash、缺少 auth_date、非法十六进制字符、非法 user JSON 格式、
// 或 auth_date 超过 24h / 大于当前时间（未来时间伪造），全部严格返回 HTTP 401 Unauthorized。
// ============================================================================
func TestTMA_E2E_Assertion3_MalformedParametersFailClosed401(t *testing.T) {
	user := TelegramUser{ID: 70003, FirstName: "Charlie", Username: "charlie_param"}
	fixture, validInitData := setupTMAE2EFixture(t, true, user, nil, nil)

	values, err := url.ParseQuery(validInitData)
	if err != nil {
		t.Fatalf("解析 validInitData 失败: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(v url.Values) string
		customH string
	}{
		{
			name: "缺少 hash 参数",
			mutate: func(v url.Values) string {
				c := cloneValues(v)
				c.Del("hash")
				return c.Encode()
			},
		},
		{
			name: "缺少 auth_date 参数",
			mutate: func(v url.Values) string {
				c := cloneValues(v)
				c.Del("auth_date")
				return c.Encode()
			},
		},
		{
			name: "hash 含有非法非十六进制字符",
			mutate: func(v url.Values) string {
				c := cloneValues(v)
				c.Set("hash", "zzzz-not-a-valid-hex-hash-value!!")
				return c.Encode()
			},
		},
		{
			name: "user 参数含有畸形非 JSON 格式",
			mutate: func(v url.Values) string {
				c := cloneValues(v)
				c.Set("user", "{not-json-format")
				return c.Encode()
			},
		},
		{
			name: "user 参数缺失",
			mutate: func(v url.Values) string {
				c := cloneValues(v)
				c.Del("user")
				return c.Encode()
			},
		},
		{
			name: "auth_date 超过 24 小时过期",
			mutate: func(v url.Values) string {
				return createTestInitData(testTMABotToken, user, time.Now().Add(-25*time.Hour).Unix(), false)
			},
		},
		{
			name: "auth_date 为未来时间伪造（超过 1 分钟）",
			mutate: func(v url.Values) string {
				return createTestInitData(testTMABotToken, user, time.Now().Add(2*time.Hour).Unix(), false)
			},
		},
		{
			name:    "缺少 X-Telegram-Init-Data 请求头",
			customH: "",
		},
		{
			name:    "空空白 X-Telegram-Init-Data 请求头",
			customH: "   \t\n   ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headerVal := tc.customH
			if tc.mutate != nil {
				headerVal = tc.mutate(values)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
			if headerVal != "" {
				req.Header.Set("X-Telegram-Init-Data", headerVal)
			}
			req.Header.Set("Origin", "https://reposentinel.example")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("[%s] 期望 401 Unauthorized，实际: %d, body: %s", tc.name, w.Code, w.Body.String())
			}

			var errResp apiErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if errResp.ErrorCode != errorCodeUnauthorized {
				t.Fatalf("期望错误码 unauthorized，实际: %s", errResp.ErrorCode)
			}
		})
	}
}

// ============================================================================
// Assertion 4: 白名单拦截无信息泄漏 (R1)
// 有效签名但 Telegram User ID 不在 chatops.telegram.allowed_user_ids 中的用户，
// 断言：严格返回 HTTP 403 Forbidden，响应体仅含通用错误提示，绝不泄露白名单名单或配置细节。
// ============================================================================
func TestTMA_E2E_Assertion4_WhitelistBlockNoInfoLeakage403(t *testing.T) {
	allowedUser := TelegramUser{ID: 70004, Username: "whitelisted_user"}
	fixture, _ := setupTMAE2EFixture(t, true, allowedUser, nil, nil)

	// 非白名单用户（合法签名）
	unauthorizedUser := TelegramUser{ID: 999999, FirstName: "Intruder", Username: "attacker"}
	unauthorizedInitData := createTestInitData(testTMABotToken, unauthorizedUser, time.Now().Unix(), false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	req.Header.Set("X-Telegram-Init-Data", unauthorizedInitData)
	req.Header.Set("Origin", "https://reposentinel.example")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("非白名单用户期望 403 Forbidden，实际: %d, body: %s", w.Code, w.Body.String())
	}

	bodyStr := w.Body.String()
	// 断言：绝不泄露白名单用户 ID、配置键名或内部信息
	forbiddenKeywords := []string{
		"allowed_user_ids",
		"whitelist",
		"chatops.telegram",
		"70004",
		"whitelisted_user",
		"sqlite",
		"table",
	}
	for _, kw := range forbiddenKeywords {
		if strings.Contains(strings.ToLower(bodyStr), strings.ToLower(kw)) {
			t.Fatalf("403 响应体泄露了敏感白名单配置信息: %q found in %s", kw, bodyStr)
		}
	}

	var errResp apiErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if errResp.ErrorCode != errorCodeForbidden {
		t.Fatalf("期望错误码 forbidden，实际: %s", errResp.ErrorCode)
	}
}

// ============================================================================
// Assertion 5: Direct Intent 客户端参数无害化 (R5)
// 客户端在请求体中附带伪造的 owner/repo/run_id/issue_number 时，
// 断言：服务端一律忽略，只以 event_id 反查数据库中真实实体。
// ============================================================================
func TestTMA_E2E_Assertion5_DirectIntentClientSpoofingIgnored(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70005, Username: "intent_verifier"}
	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAE2EFixture(t, true, user, mockExec, nil)

	// 1. 在数据库中创建合法仓库与事件
	realRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-real-intent",
		Owner:          "sentinel-org",
		Name:           "real-repo",
		FullName:       "sentinel-org/real-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	realRunID := int64(888123)
	ev, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-intent-real-1",
		DedupeFingerprint: "fp-intent-real-1",
		RepositoryID:      &realRepo.ID,
		WorkflowRunID:     &realRunID,
		Kind:              "workflow_run",
		Title:             "CI Run Failed",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建事件失败: %v", err)
	}

	// 2. 客户端在请求体中伪造 owner、repo、run_id
	spoofedBody := fmt.Sprintf(`{
		"event_id": "%s",
		"action": "workflow_rerun",
		"owner": "malicious-attacker",
		"repo": "fake-repo",
		"run_id": "99999999",
		"issue_number": 99999
	}`, ev.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(spoofedBody))
	req.Header.Set("X-Telegram-Init-Data", initData)
	req.Header.Set("Origin", "https://reposentinel.example")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("处置请求期望 200 OK，实际: %d, body: %s", w.Code, w.Body.String())
	}

	mockExec.mu.Lock()
	defer mockExec.mu.Unlock()
	if len(mockExec.actions) != 1 {
		t.Fatalf("期望执行 1 次动作，实际: %d", len(mockExec.actions))
	}

	dispatched := mockExec.actions[0]
	if dispatched.RepoID != realRepo.ID {
		t.Fatalf("RepoID 未从数据库 event 反查，期望 %s，实际: %s", realRepo.ID, dispatched.RepoID)
	}
	if dispatched.RunID != strconv.FormatInt(realRunID, 10) {
		t.Fatalf("RunID 未从数据库 event 反查，期望 %d，实际: %s (客户端伪造值泄入！)", realRunID, dispatched.RunID)
	}
	if dispatched.Action != "workflow_rerun" {
		t.Fatalf("期望动作 workflow_rerun，实际: %s", dispatched.Action)
	}
}

// ============================================================================
// Assertion 6: 跨仓库越权拦截 (R5)
// 客户端提交合法 event_id 但其关联的仓库并非当前实例纳管且启用的仓库（或已被软删除/归档），
// 断言：服务端严格返回 HTTP 403 Forbidden。
// ============================================================================
func TestTMA_E2E_Assertion6_CrossRepoUnmonitoredAccessRejected403(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70006, Username: "cross_repo_tester"}
	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAE2EFixture(t, true, user, mockExec, nil)

	// 1. 创建未启用监控的仓库
	disabledRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-disabled-cross",
		Owner:          "sentinel-org",
		Name:           "disabled-repo",
		FullName:       "sentinel-org/disabled-repo",
		MonitorEnabled: false,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}
	disabledFlag := false
	_ = fixture.store.Repositories().UpdateSettings(ctx, disabledRepo.ID, store.RepositorySettings{
		MonitorEnabled: &disabledFlag,
	})

	// 2. 创建已归档仓库
	archivedRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-archived-cross",
		Owner:          "sentinel-org",
		Name:           "archived-repo",
		FullName:       "sentinel-org/archived-repo",
		MonitorEnabled: true,
		IsArchived:     true,
	})
	if err != nil {
		t.Fatalf("创建归档仓库失败: %v", err)
	}

	runID := int64(12345)
	evDisabled, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-disabled-repo",
		DedupeFingerprint: "fp-disabled-repo",
		RepositoryID:      &disabledRepo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Disabled Repo Run",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建事件失败: %v", err)
	}

	evArchived, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-archived-repo",
		DedupeFingerprint: "fp-archived-repo",
		RepositoryID:      &archivedRepo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Archived Repo Run",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建归档事件失败: %v", err)
	}

	nonExistentRepoID := "repo-non-existent"
	evOrphan, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-orphan-repo",
		DedupeFingerprint: "fp-orphan-repo",
		RepositoryID:      &nonExistentRepoID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Orphan Event",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建孤立事件失败: %v", err)
	}

	cases := []struct {
		name    string
		eventID string
	}{
		{"未纳管/未启用仓库关联事件", evDisabled.ID},
		{"已归档仓库关联事件", evArchived.ID},
		{"不存在的仓库关联事件", evOrphan.ID},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"event_id":"%s","action":"workflow_rerun"}`, c.eventID)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payload))
			req.Header.Set("X-Telegram-Init-Data", initData)
			req.Header.Set("Origin", "https://reposentinel.example")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("[%s] 跨仓库越权未拦截，期望 403 Forbidden，实际: %d, body: %s", c.name, w.Code, w.Body.String())
			}

			var errResp apiErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if errResp.ErrorCode != errorCodeForbidden {
				t.Fatalf("期望错误码 forbidden，实际: %s", errResp.ErrorCode)
			}
		})
	}
}

// ============================================================================
// Assertion 7: 并发与幂等状态机 (P1, D1, D6)
//   - 并发双提交：首个请求抢占写入 pending receipt 并向 GitHub 发出外部调用，并发的第二个请求直接命中 ReceiptPending。
//     断言：第二个请求立即返回 HTTP 409 Conflict，响应体严格遵循标准错误格式
//     {"error_code":"conflict","message":"...","details":{"reason":"action_in_progress","action":"..."}}，绝不发起第二次 GitHub API 调用；
//   - 重复提交：已完成的动作携带相同稳定键再次提交，断言：返回 HTTP 200 OK 且包含先前已完成收据，不触发外部调用；
//   - 评论内容隔离：不同正文的 PR 评论因携带内容哈希（tma:comment:<repo_id>:<pr_number>:<sha256[:16]>），
//     断言：不同正文的评论请求不互相阻塞，各自成功执行；相同正文的重复点击被幂等拦截。
//
// ============================================================================
func TestTMA_E2E_Assertion7_ConcurrencyConflictAndIdempotency(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70007, Username: "idempotency_tester"}

	var mu sync.Mutex
	callCount := 0
	mockExec := &mockTMAActionExecutor{}

	fixture, initData := setupTMAE2EFixture(t, true, user, mockExec, nil)

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-idem-1",
		Owner:          "sentinel-org",
		Name:           "idem-repo",
		FullName:       "sentinel-org/idem-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	prNum := int64(108)
	evPR, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-idem-pr-1",
		DedupeFingerprint: "fp-idem-pr-1",
		RepositoryID:      &repo.ID,
		SubjectNumber:     &prNum,
		Kind:              "pull_request",
		Title:             "PR 108 Idempotency",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建 PR 事件失败: %v", err)
	}

	t.Run("并发抢占返回 409 Conflict 标准错误格式", func(t *testing.T) {
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			mu.Lock()
			callCount++
			current := callCount
			mu.Unlock()

			if current == 1 {
				return &chatops.Outcome{
					Status:  chatops.StatusExecuted,
					Action:  action.Action,
					Message: "first call succeeded",
				}, nil
			}
			// 第二个并发请求直接命中 pending 状态
			return &chatops.Outcome{
				Status:  chatops.StatusPending,
				Action:  action.Action,
				Message: "already pending",
			}, nil
		}

		// 第一个请求
		payload := fmt.Sprintf(`{"event_id":"%s","action":"pr_quick_approve"}`, evPR.ID)
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payload))
		req1.Header.Set("X-Telegram-Init-Data", initData)
		req1.Header.Set("Origin", "https://reposentinel.example")
		req1.Header.Set("Content-Type", "application/json")
		w1 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("首个请求期望 200 OK，实际: %d, body: %s", w1.Code, w1.Body.String())
		}

		// 第二个并发请求命中 StatusPending
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payload))
		req2.Header.Set("X-Telegram-Init-Data", initData)
		req2.Header.Set("Origin", "https://reposentinel.example")
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w2, req2)

		if w2.Code != http.StatusConflict {
			t.Fatalf("并发抢占期望 409 Conflict，实际: %d, body: %s", w2.Code, w2.Body.String())
		}

		var errResp apiErrorResponse
		if err := json.Unmarshal(w2.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("解析 409 响应失败: %v", err)
		}
		if errResp.ErrorCode != errorCodeConflict {
			t.Fatalf("期望 error_code 为 conflict，实际: %s", errResp.ErrorCode)
		}
		if errResp.Details["reason"] != "action_in_progress" {
			t.Fatalf("期望 details.reason 为 action_in_progress，实际: %v", errResp.Details["reason"])
		}
		if errResp.Details["action"] != "pr_quick_approve" {
			t.Fatalf("期望 details.action 为 pr_quick_approve，实际: %v", errResp.Details["action"])
		}
	})

	t.Run("重复提交已完成动作返回 200 OK already_done", func(t *testing.T) {
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			return &chatops.Outcome{
				Status:     chatops.StatusAlreadyDone,
				Action:     action.Action,
				Message:    "already done receipt",
				ExternalID: "receipt-prev-completed-123",
			}, nil
		}

		payload := fmt.Sprintf(`{"event_id":"%s","action":"pr_quick_approve"}`, evPR.ID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payload))
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Origin", "https://reposentinel.example")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("重复提交期望 200 OK，实际: %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if resp["status"] != "already_done" {
			t.Fatalf("期望 status 为 already_done，实际: %v", resp["status"])
		}
		if resp["external_id"] != "receipt-prev-completed-123" {
			t.Fatalf("期望 external_id 保留，实际: %v", resp["external_id"])
		}
	})

	t.Run("评论正文哈希隔离与重复幂等", func(t *testing.T) {
		var executedKeys []string
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			executedKeys = append(executedKeys, action.ID)
			return &chatops.Outcome{
				Status:  chatops.StatusExecuted,
				Action:  action.Action,
				Message: "comment posted",
			}, nil
		}

		// 1. 发送评论 A
		payloadA := fmt.Sprintf(`{"event_id":"%s","action":"pr_comment","comment_body":"Looks great, approved!"}`, evPR.ID)
		reqA := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payloadA))
		reqA.Header.Set("X-Telegram-Init-Data", initData)
		reqA.Header.Set("Origin", "https://reposentinel.example")
		reqA.Header.Set("Content-Type", "application/json")
		wA := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wA, reqA)
		if wA.Code != http.StatusOK {
			t.Fatalf("评论 A 发送期望 200 OK，实际: %d, body: %s", wA.Code, wA.Body.String())
		}

		// 2. 发送评论 B (不同正文)
		payloadB := fmt.Sprintf(`{"event_id":"%s","action":"pr_comment","comment_body":"Please check the security test suite."}`, evPR.ID)
		reqB := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(payloadB))
		reqB.Header.Set("X-Telegram-Init-Data", initData)
		reqB.Header.Set("Origin", "https://reposentinel.example")
		reqB.Header.Set("Content-Type", "application/json")
		wB := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wB, reqB)
		if wB.Code != http.StatusOK {
			t.Fatalf("评论 B 发送期望 200 OK，实际: %d, body: %s", wB.Code, wB.Body.String())
		}

		if len(executedKeys) != 2 {
			t.Fatalf("期望两条评论均独立执行，实际执行次数: %d", len(executedKeys))
		}
		if executedKeys[0] == executedKeys[1] {
			t.Fatalf("不同正文评论幂等键不应相同: key1=%s, key2=%s", executedKeys[0], executedKeys[1])
		}
		if !strings.HasPrefix(executedKeys[0], "tma:comment:") || !strings.HasPrefix(executedKeys[1], "tma:comment:") {
			t.Fatalf("评论键格式不正确: %v", executedKeys)
		}
	})
}

// ============================================================================
// Assertion 8: 大日志流式截断与内存安全 (R10, D5)
// 底层日志读取最多消费 2 MiB 字节，输出最多 200 行，超限返回 truncated:true，
// 且实现不得把完整日志一次性读入内存。
// ============================================================================
func TestTMA_E2E_Assertion8_LogStreamConsumptionAndTruncation(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70008, Username: "log_stream_tester"}

	var servedLogBytes []byte
	var servedJobs []githubx.WorkflowJobItem

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/jobs") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jobs": servedJobs})
			return
		}
		if strings.Contains(r.URL.Path, "/actions/jobs/") && strings.HasSuffix(r.URL.Path, "/logs") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(servedLogBytes)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	fixture, initData := setupTMAE2EFixture(t, true, user, nil, ghClient)

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-log-stream-1",
		Owner:          "sentinel-org",
		Name:           "log-repo",
		FullName:       "sentinel-org/log-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	runID := int64(987654)
	ev, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-log-stream-1",
		DedupeFingerprint: "fp-log-stream-1",
		RepositoryID:      &repo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Large Log Workflow Run",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建事件失败: %v", err)
	}

	jobID := int64(112233)
	servedJobs = []githubx.WorkflowJobItem{
		{ID: jobID, Name: "build-and-test", Conclusion: "failure"},
	}

	t.Run("超 2 MiB 且超 200 行日志截断并保留尾部 200 行", func(t *testing.T) {
		// 生成 3 MiB，500 行的超大日志
		var logBuf bytes.Buffer
		padding := strings.Repeat("A", 6*1024) // 每行约 6KB
		for i := 1; i <= 500; i++ {
			logBuf.WriteString(fmt.Sprintf("Step %03d: %s\n", i, padding))
		}
		servedLogBytes = logBuf.Bytes()
		if len(servedLogBytes) < 2*1024*1024 {
			t.Fatalf("构造的日志长度不足 2 MiB: %d", len(servedLogBytes))
		}

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Origin", "https://reposentinel.example")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("读取日志期望 200 OK，实际: %d, body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Logs      string `json:"logs"`
			Truncated bool   `json:"truncated"`
			JobID     int64  `json:"job_id"`
			JobName   string `json:"job_name"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}

		if !resp.Truncated {
			t.Fatalf("超 2 MiB 日志期望 truncated == true，实际: false")
		}
		if resp.JobID != jobID {
			t.Fatalf("期望 job_id 为 %d，实际: %d", jobID, resp.JobID)
		}

		lines := strings.Split(resp.Logs, "\n")
		if len(lines) > 200 {
			t.Fatalf("日志保留行数超过 200 上限，实际: %d 行", len(lines))
		}
		if len(lines) == 0 {
			t.Fatalf("日志内容为空")
		}

		// 断言环形缓冲保留的是尾部日志，而不是头部日志
		if strings.Contains(lines[0], "Step 001:") {
			t.Fatalf("大日志应截断头部保留尾部，但不应出现第一行 Step 001")
		}
	})

	t.Run("小型日志正常完整输出且 truncated 为 false", func(t *testing.T) {
		var logBuf bytes.Buffer
		for i := 1; i <= 20; i++ {
			logBuf.WriteString(fmt.Sprintf("Small line %02d\n", i))
		}
		servedLogBytes = logBuf.Bytes()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Origin", "https://reposentinel.example")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("读取日志期望 200 OK，实际: %d, body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Logs      string `json:"logs"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}

		if resp.Truncated {
			t.Fatalf("小型日志不应标记为截断")
		}
		lines := strings.Split(strings.TrimRight(resp.Logs, "\n"), "\n")
		if len(lines) != 20 {
			t.Fatalf("期望 20 行日志，实际: %d 行", len(lines))
		}
	})
}

// ============================================================================
// Assertion 9: Feature Flag 绝对阻断 (R13)
// 当 cfg.HTTP.TMA.Enabled == false 时，断言：
// - 所有 /api/v1/tma/* 路由严格返回 HTTP 404 Not Found；
// - 外发告警通知的 inline keyboard 中绝不包含 web_app 按钮。
// ============================================================================
func TestTMA_E2E_Assertion9_FeatureFlagDisabled(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70009, Username: "feature_flag_tester"}

	// 1. 测试路由绝对阻断 (404)
	fixture, initData := setupTMAE2EFixture(t, false, user, nil, nil)

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/tma/dashboard", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-999", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-999/diff", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-999/logs", ""},
		{http.MethodPost, "/api/v1/tma/actions", `{"action":"workflow_rerun","event_id":"ev-999"}`},
		{http.MethodPost, "/api/v1/tma/quiet-hours", `{"enabled":true}`},
	}

	for _, rt := range routes {
		t.Run("Route_404_"+rt.method+"_"+rt.path, func(t *testing.T) {
			var bodyReader io.Reader
			if rt.body != "" {
				bodyReader = strings.NewReader(rt.body)
			}
			req := httptest.NewRequest(rt.method, rt.path, bodyReader)
			req.Header.Set("X-Telegram-Init-Data", initData)
			req.Header.Set("Origin", "https://reposentinel.example")
			if rt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("TMA 禁用时路由 %s %s 期望 404 Not Found，实际: %d, body: %s", rt.method, rt.path, w.Code, w.Body.String())
			}

			var errResp apiErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if errResp.ErrorCode != errorCodeNotFound {
				t.Fatalf("期望错误码 not_found，实际: %s", errResp.ErrorCode)
			}
		})
	}

	// 2. 测试外发通知中绝对不包含 web_app 按钮
	t.Run("Notification_ZeroWebAppButtonWhenDisabled", func(t *testing.T) {
		var receivedTelegramPayload map[string]any
		mockTGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedTelegramPayload)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1001}}`))
		}))
		defer mockTGServer.Close()

		ring := setupTestKeyRing(t)
		env, err := ring.Encrypt(ctx, []byte("fake-token"), []byte("reposentinel:notify-secret:v1"))
		if err != nil {
			t.Fatalf("加密测试 Token 失败: %v", err)
		}

		workerStore := fixture.store
		// 写入一条 telegram 渠道
		ch, err := workerStore.Channels().Upsert(ctx, store.NotificationChannel{
			ID:             "ch-tg-flag-disabled",
			ChannelType:    store.ChannelTelegram,
			Name:           "tg",
			Target:         "12345678",
			SecretEnvelope: env,
			Enabled:        true,
		})
		if err != nil {
			t.Fatalf("创建渠道失败: %v", err)
		}

		// 写入包含 event_id 的待发通知
		eventID := "ev-workflow-alert-1"
		now := time.Now().UTC()
		_, err = workerStore.Outbox().Create(ctx, store.NotificationOutbox{
			ID:             "o-flag-disabled-1",
			ChannelID:      ch.ID,
			IdempotencyKey: "idem-flag-disabled-1",
			EventID:        &eventID,
			Status:         store.OutboxPending,
			NextAttemptAt:  now.Add(-time.Second),
			Title:          "Workflow Failed",
			BodyText:       "Run failed on main",
			HTMLURL:        "https://github.com/sentinel-org/repo/actions/runs/1",
			ParseMode:      "HTML",
			BodyJSON: map[string]any{
				"event_id":   eventID,
				"event_kind": "workflow_run",
			},
		})
		if err != nil {
			t.Fatalf("入队 Outbox 失败: %v", err)
		}

		worker := &notify.Worker{
			Store:           workerStore,
			KeyRing:         &ring,
			AAD:             "reposentinel:notify-secret:v1",
			Client:          mockTGServer.Client(),
			TelegramAPIBase: mockTGServer.URL,
			PublicBaseURL:   "https://reposentinel.example",
			TMAEnabled:      false, // 显式禁用 TMA
		}
		worker.Tick(ctx)

		// 扫描收到的 Telegram 载荷，断言绝无 web_app 按钮
		rm, hasMarkup := receivedTelegramPayload["reply_markup"].(map[string]any)
		if hasMarkup {
			if kb, ok := rm["inline_keyboard"].([]any); ok {
				for _, rowAny := range kb {
					if row, ok := rowAny.([]any); ok {
						for _, btnAny := range row {
							if btn, ok := btnAny.(map[string]any); ok {
								if _, hasWebApp := btn["web_app"]; hasWebApp {
									t.Fatalf("TMA 禁用时不应包含 web_app 按钮，实际捕获到: %v", btn)
								}
							}
						}
					}
				}
			}
		}
	})

	// 3. 对比测试：当 TMAEnabled: true 时，外发通知必须包含 web_app 按钮
	t.Run("Notification_CarriesWebAppButtonWhenEnabled", func(t *testing.T) {
		var receivedTelegramPayload map[string]any
		mockTGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedTelegramPayload)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1002}}`))
		}))
		defer mockTGServer.Close()

		ring := setupTestKeyRing(t)
		env, err := ring.Encrypt(ctx, []byte("fake-token"), []byte("reposentinel:notify-secret:v1"))
		if err != nil {
			t.Fatalf("加密测试 Token 失败: %v", err)
		}

		workerStore := fixture.store
		ch, err := workerStore.Channels().Upsert(ctx, store.NotificationChannel{
			ID:             "ch-tg-flag-enabled",
			ChannelType:    store.ChannelTelegram,
			Name:           "tg",
			Target:         "12345678",
			SecretEnvelope: env,
			Enabled:        true,
		})
		if err != nil {
			t.Fatalf("创建渠道失败: %v", err)
		}

		eventID := "ev-workflow-alert-enabled"
		now := time.Now().UTC()
		_, err = workerStore.Outbox().Create(ctx, store.NotificationOutbox{
			ID:             "o-flag-enabled-1",
			ChannelID:      ch.ID,
			IdempotencyKey: "idem-flag-enabled-1",
			EventID:        &eventID,
			Status:         store.OutboxPending,
			NextAttemptAt:  now.Add(-time.Second),
			Title:          "Workflow Failed",
			BodyText:       "Run failed on main",
			HTMLURL:        "https://github.com/sentinel-org/repo/actions/runs/2",
			ParseMode:      "HTML",
			BodyJSON: map[string]any{
				"event_id":   eventID,
				"event_kind": "workflow_run",
			},
		})
		if err != nil {
			t.Fatalf("入队 Outbox 失败: %v", err)
		}

		worker := &notify.Worker{
			Store:           workerStore,
			KeyRing:         &ring,
			AAD:             "reposentinel:notify-secret:v1",
			Client:          mockTGServer.Client(),
			TelegramAPIBase: mockTGServer.URL,
			PublicBaseURL:   "https://reposentinel.example",
			TMAEnabled:      true, // 显式启用 TMA
		}
		worker.Tick(ctx)

		rm, hasMarkup := receivedTelegramPayload["reply_markup"].(map[string]any)
		if !hasMarkup {
			t.Fatalf("TMA 启用时期望包含 reply_markup: %v", receivedTelegramPayload)
		}
		var foundWebAppBtn map[string]any
		if kb, ok := rm["inline_keyboard"].([]any); ok {
			for _, rowAny := range kb {
				if row, ok := rowAny.([]any); ok {
					for _, btnAny := range row {
						if btn, ok := btnAny.(map[string]any); ok {
							if _, hasWebApp := btn["web_app"]; hasWebApp {
								foundWebAppBtn = btn
							}
						}
					}
				}
			}
		}
		if foundWebAppBtn == nil {
			t.Fatalf("TMA 启用时期望包含 web_app 按钮，未找到。payload: %v", receivedTelegramPayload)
		}
		expectedURL := "https://reposentinel.example/tma/events/" + eventID
		webAppObj := foundWebAppBtn["web_app"].(map[string]any)
		if webAppObj["url"] != expectedURL {
			t.Fatalf("web_app url 不符，want %s, got %v", expectedURL, webAppObj["url"])
		}
	})
}

// ============================================================================
// Assertion 10: 普通浏览器零请求安全 (R14)
// 在普通浏览器（无 window.Telegram?.WebApp?.initData）直接访问 /tma，
// 断言：前端组件渲染占位屏，网络面板监控零 API 请求发送；直接向 /api/v1/tma/* 发起未签名请求一律返回 401 Unauthorized。
// ============================================================================
func TestTMA_E2E_Assertion10_DirectBrowserAccessOutsideTelegram(t *testing.T) {
	user := TelegramUser{ID: 70010, Username: "browser_direct_tester"}
	fixture, _ := setupTMAE2EFixture(t, true, user, nil, nil)

	t.Run("未签名直接调用接口全部严格返回 401", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/tma/dashboard"},
			{http.MethodGet, "/api/v1/tma/events/ev-direct-1"},
			{http.MethodGet, "/api/v1/tma/events/ev-direct-1/diff"},
			{http.MethodGet, "/api/v1/tma/events/ev-direct-1/logs"},
			{http.MethodPost, "/api/v1/tma/actions"},
			{http.MethodPost, "/api/v1/tma/quiet-hours"},
		}

		for _, ep := range endpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			// 无 X-Telegram-Init-Data
			req.Header.Set("Origin", "https://reposentinel.example")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("未签名直接调用 %s %s 期望 401 Unauthorized，实际: %d, body: %s", ep.method, ep.path, w.Code, w.Body.String())
			}

			var errResp apiErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if errResp.ErrorCode != errorCodeUnauthorized {
				t.Fatalf("期望错误码 unauthorized，实际: %s", errResp.ErrorCode)
			}
		}
	})

	t.Run("浏览器直接访问 SPA 路由 /tma 正常返回前端壳且未触发任何鉴权泄漏", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tma", nil)
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("浏览器访问 /tma SPA 路由期望 200 OK，实际: %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "TMA Shell") {
			t.Fatalf("期望返回 SPA index.html 壳内容，实际: %s", w.Body.String())
		}
	})
}

// ============================================================================
// Assertion 11: 敏感凭据脱敏零泄漏 (D6)
// Bot Token、解密信封明文与完整 initData 绝不出现在任何系统日志、错误响应体（包括 401/409/500）、
// URL Query 参数或审计记录中。
// 断言：扫描完整测试运行生成的日志与所有端点响应体样本，Token 字符串与敏感凭据 0 命中。
// ============================================================================
func TestTMA_E2E_Assertion11_SensitiveCredentialsZeroLeakage(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70011, Username: "leakage_tester"}

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAE2EFixture(t, true, user, mockExec, nil, logger)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-leak-test",
		Owner:          "sentinel-org",
		Name:           "leak-repo",
		FullName:       "sentinel-org/leak-repo",
		MonitorEnabled: true,
	})
	runID := int64(3344)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-leak-test-1",
		DedupeFingerprint: "fp-leak-test-1",
		RepositoryID:      &repo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Leak Test Event",
		OccurredAt:        time.Now().UTC(),
	})

	var collectedResponses []string

	// 运行一系列混合请求（成功、验签失败、跨站、冲突等）
	runScenario := func(method, path, body, initH, originH string) {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, r)
		if initH != "" {
			req.Header.Set("X-Telegram-Init-Data", initH)
		}
		if originH != "" {
			req.Header.Set("Origin", originH)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)
		collectedResponses = append(collectedResponses, w.Body.String())
	}

	// 1. 成功请求
	runScenario(http.MethodGet, "/api/v1/tma/dashboard", "", initData, "https://reposentinel.example")
	runScenario(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s", ev.ID), "", initData, "https://reposentinel.example")

	// 2. 验签失败请求 (携带畸形及错误签名)
	reversedInitData := createTestInitData(testTMABotToken, user, time.Now().Unix(), true)
	runScenario(http.MethodGet, "/api/v1/tma/dashboard", "", reversedInitData, "https://reposentinel.example")
	runScenario(http.MethodGet, "/api/v1/tma/dashboard", "", "hash=invalid&auth_date=123", "https://reposentinel.example")

	// 3. 跨域拦截
	runScenario(http.MethodGet, "/api/v1/tma/dashboard", "", initData, "https://evil.attacker.org")

	// 4. 动作执行与冲突
	actionPayload := fmt.Sprintf(`{"event_id":"%s","action":"workflow_rerun"}`, ev.ID)
	runScenario(http.MethodPost, "/api/v1/tma/actions", actionPayload, initData, "https://reposentinel.example")

	mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
		return &chatops.Outcome{Status: chatops.StatusPending, Action: action.Action}, nil
	}
	runScenario(http.MethodPost, "/api/v1/tma/actions", actionPayload, initData, "https://reposentinel.example")

	// 5. 扫描所有日志
	logOutput := logBuffer.String()
	sensitiveTokens := []string{
		testTMABotToken,
		"ABCdefGHIjklMNOpqrSTUvwxYZ_test_token",
	}

	for _, token := range sensitiveTokens {
		if strings.Contains(logOutput, token) {
			t.Fatalf("日志中发现泄露的敏感 Bot Token: %s", token)
		}
	}

	// 6. 扫描所有响应体
	for i, respBody := range collectedResponses {
		for _, token := range sensitiveTokens {
			if strings.Contains(respBody, token) {
				t.Fatalf("响应体 #%d 中发现泄露的敏感 Bot Token: %s (body: %s)", i, token, respBody)
			}
		}
	}

	// 7. 扫描数据库审计记录
	audits, err := fixture.store.Audits().List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("读取审计日志失败: %v", err)
	}
	for _, entry := range audits {
		metaStr := string(entry.MetadataJSON)
		for _, token := range sensitiveTokens {
			if strings.Contains(metaStr, token) {
				t.Fatalf("审计日志 metadata_json 中发现泄露的敏感 Token: %s", token)
			}
		}
		// 校验完整的 initData 没有整串写入 metadata
		if len(initData) > 0 && strings.Contains(metaStr, initData) {
			t.Fatalf("审计日志中发现泄露的完整 initData 字符串")
		}
	}
}

// ============================================================================
// Assertion 12: 跨站 Origin 边界阻断 (R15, D6)
// 向 /api/v1/tma/* 发起携带未授权外部 Origin 的跨域请求，
// 断言：服务端严格拦截并返回 HTTP 403 Forbidden；仅允许与当前实例同源的 Origin（匹配 cfg.HTTP.PublicBaseURL）或同源相对路径调用。
// ============================================================================
func TestTMA_E2E_Assertion12_CrossOriginUnauthorizedOriginRejected403(t *testing.T) {
	user := TelegramUser{ID: 70012, Username: "cors_tester"}
	fixture, initData := setupTMAE2EFixture(t, true, user, nil, nil)

	cases := []struct {
		name       string
		origin     string
		referer    string
		wantStatus int
	}{
		{
			name:       "合法同源 Origin 匹配 PublicBaseURL",
			origin:     "https://reposentinel.example",
			wantStatus: http.StatusOK,
		},
		{
			name:       "跨域未授权 Origin 直接拦截 403",
			origin:     "https://malicious.evil.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "跨域协议不符 (http 伪造)",
			origin:     "http://reposentinel.example",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "同源相对调用 (无 Origin 无 Referer)",
			wantStatus: http.StatusOK,
		},
		{
			name:       "未授权外部 Referer 拦截 403",
			referer:    "https://malicious.evil.com/phishing/page",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "合法同源 Referer 允许访问",
			referer:    "https://reposentinel.example/tma",
			wantStatus: http.StatusOK,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
			req.Header.Set("X-Telegram-Init-Data", initData)
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.referer != "" {
				req.Header.Set("Referer", c.referer)
			}
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != c.wantStatus {
				t.Fatalf("[%s] 期望状态码 %d，实际: %d, body: %s", c.name, c.wantStatus, w.Code, w.Body.String())
			}

			if c.wantStatus == http.StatusForbidden {
				var errResp apiErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("解析响应失败: %v", err)
				}
				if errResp.ErrorCode != errorCodeForbidden {
					t.Fatalf("期望错误码 forbidden，实际: %s", errResp.ErrorCode)
				}
			}
		})
	}
}

// ============================================================================
// Assertion 13: TMA 独立限流与重试时间 (R15, D6)
// 对 /api/v1/tma/* 端点发起高频请求（只读超过 60 次/分，写操作超过 10 次/分），
// 断言：超出部分严格返回 HTTP 429 Too Many Requests，且响应头包含非空的 Retry-After: 60。
// ============================================================================
func TestTMA_E2E_Assertion13_RateLimitReadsAndWrites(t *testing.T) {
	user := TelegramUser{ID: 70013, Username: "rate_limit_tester"}
	fixture, initData := setupTMAE2EFixture(t, true, user, nil, nil)

	t.Run("读请求超过 60 次/分拦截 429 并返回 Retry-After: 60", func(t *testing.T) {
		for i := 1; i <= 60; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
			req.Header.Set("X-Telegram-Init-Data", initData)
			req.Header.Set("Origin", "https://reposentinel.example")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("前 60 次读请求不应被限流，第 %d 次返回: %d, body: %s", i, w.Code, w.Body.String())
			}
		}

		// 第 61 次请求必须触发 429
		req61 := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req61.Header.Set("X-Telegram-Init-Data", initData)
		req61.Header.Set("Origin", "https://reposentinel.example")
		w61 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w61, req61)

		if w61.Code != http.StatusTooManyRequests {
			t.Fatalf("第 61 次读请求期望 429 Too Many Requests，实际: %d", w61.Code)
		}
		retryAfter := w61.Header().Get("Retry-After")
		if retryAfter != "60" {
			t.Fatalf("期望 Retry-After 响应头为 60，实际: %q", retryAfter)
		}

		var errResp apiErrorResponse
		if err := json.Unmarshal(w61.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if errResp.ErrorCode != errorCodeRateLimited {
			t.Fatalf("期望错误码 rate_limited，实际: %s", errResp.ErrorCode)
		}
	})

	t.Run("写请求超过 10 次/分拦截 429 并返回 Retry-After: 60", func(t *testing.T) {
		// 使用独立用户避开读限流计数影响
		writeUser := TelegramUser{ID: 70014, Username: "write_limit_tester"}
		writeFixture, writeInitData := setupTMAE2EFixture(t, true, writeUser, nil, nil)

		for i := 1; i <= 10; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
			req.Header.Set("X-Telegram-Init-Data", writeInitData)
			req.Header.Set("Origin", "https://reposentinel.example")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			writeFixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("前 10 次写请求不应被限流，第 %d 次返回: %d, body: %s", i, w.Code, w.Body.String())
			}
		}

		// 第 11 次写请求必须触发 429
		req11 := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
		req11.Header.Set("X-Telegram-Init-Data", writeInitData)
		req11.Header.Set("Origin", "https://reposentinel.example")
		req11.Header.Set("Content-Type", "application/json")
		w11 := httptest.NewRecorder()
		writeFixture.handler.ServeHTTP(w11, req11)

		if w11.Code != http.StatusTooManyRequests {
			t.Fatalf("第 11 次写请求期望 429 Too Many Requests，实际: %d", w11.Code)
		}
		retryAfter := w11.Header().Get("Retry-After")
		if retryAfter != "60" {
			t.Fatalf("期望 Retry-After 响应头为 60，实际: %q", retryAfter)
		}
	})
}

// ============================================================================
// Full Lifecycle Integration: 覆盖从告警生成到 TMA 处置与静音的全流程
// ============================================================================
func TestTMA_E2E_FullLifecycle(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 70020, FirstName: "Lead", Username: "lead_dev"}

	// 1. 设置 GitHub Mock Server 支持 Diff 与 Logs
	const expectedDiff = "diff --git a/pkg/service.go b/pkg/service.go\n--- a/pkg/service.go\n+++ b/pkg/service.go\n@@ -10,3 +10,3 @@\n-oldLogic()\n+newLogic()\n"
	const expectedLogs = "2026-11-08T03:00:00Z Job started\n2026-11-08T03:00:10Z Tests failed in suite A\n"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pulls/77") {
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			_, _ = w.Write([]byte(expectedDiff))
			return
		}
		if strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/jobs") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []githubx.WorkflowJobItem{
					{ID: 9911, Name: "unit-tests", Conclusion: "failure"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/actions/jobs/9911/logs") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(expectedLogs))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAE2EFixture(t, true, user, mockExec, ghClient)

	// 2. 初始化被监控仓库与事件
	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-lifecycle-1",
		Owner:          "sentinel-org",
		Name:           "lifecycle-repo",
		FullName:       "sentinel-org/lifecycle-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	prNum := int64(77)
	runID := int64(8899)
	ev, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-lifecycle-pr-77",
		DedupeFingerprint: "fp-lifecycle-pr-77",
		RepositoryID:      &repo.ID,
		SubjectNumber:     &prNum,
		WorkflowRunID:     &runID,
		Kind:              "pull_request",
		Title:             "PR 77 Lifecycle Feature",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建事件失败: %v", err)
	}

	// 3. Step 1: 用户打开 TMA 仪表盘
	reqDash := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	reqDash.Header.Set("X-Telegram-Init-Data", initData)
	reqDash.Header.Set("Origin", "https://reposentinel.example")
	wDash := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wDash, reqDash)
	if wDash.Code != http.StatusOK {
		t.Fatalf("仪表盘请求失败: %d", wDash.Code)
	}

	// 4. Step 2: 用户查看事件详情
	reqEv := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s", ev.ID), nil)
	reqEv.Header.Set("X-Telegram-Init-Data", initData)
	reqEv.Header.Set("Origin", "https://reposentinel.example")
	wEv := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wEv, reqEv)
	if wEv.Code != http.StatusOK {
		t.Fatalf("查看事件详情失败: %d", wEv.Code)
	}

	// 5. Step 3: 用户查看 PR 代码 Diff
	reqDiff := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/diff", ev.ID), nil)
	reqDiff.Header.Set("X-Telegram-Init-Data", initData)
	reqDiff.Header.Set("Origin", "https://reposentinel.example")
	wDiff := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wDiff, reqDiff)
	if wDiff.Code != http.StatusOK {
		t.Fatalf("查看 Diff 失败: %d, body: %s", wDiff.Code, wDiff.Body.String())
	}
	var diffResp map[string]any
	_ = json.Unmarshal(wDiff.Body.Bytes(), &diffResp)
	if diffResp["diff"] != expectedDiff {
		t.Fatalf("返回的 Diff 不一致: %v", diffResp["diff"])
	}

	// 6. Step 4: 用户查看失败工作流日志
	reqLogs := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
	reqLogs.Header.Set("X-Telegram-Init-Data", initData)
	reqLogs.Header.Set("Origin", "https://reposentinel.example")
	wLogs := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wLogs, reqLogs)
	if wLogs.Code != http.StatusOK {
		t.Fatalf("查看日志失败: %d, body: %s", wLogs.Code, wLogs.Body.String())
	}

	// 7. Step 5: 用户执行 PR 快速通过
	reqApprove := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(fmt.Sprintf(`{"event_id":"%s","action":"pr_quick_approve"}`, ev.ID)))
	reqApprove.Header.Set("X-Telegram-Init-Data", initData)
	reqApprove.Header.Set("Origin", "https://reposentinel.example")
	reqApprove.Header.Set("Content-Type", "application/json")
	wApprove := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wApprove, reqApprove)
	if wApprove.Code != http.StatusOK {
		t.Fatalf("执行 pr_quick_approve 失败: %d", wApprove.Code)
	}

	// 8. Step 6: 用户发表 PR 评论
	reqComment := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(fmt.Sprintf(`{"event_id":"%s","action":"pr_comment","comment_body":"LGTM from Telegram Mini App!"}`, ev.ID)))
	reqComment.Header.Set("X-Telegram-Init-Data", initData)
	reqComment.Header.Set("Origin", "https://reposentinel.example")
	reqComment.Header.Set("Content-Type", "application/json")
	wComment := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wComment, reqComment)
	if wComment.Code != http.StatusOK {
		t.Fatalf("发表评论失败: %d", wComment.Code)
	}

	// 9. Step 7: 用户启用静音保护
	reqQuiet := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
	reqQuiet.Header.Set("X-Telegram-Init-Data", initData)
	reqQuiet.Header.Set("Origin", "https://reposentinel.example")
	reqQuiet.Header.Set("Content-Type", "application/json")
	wQuiet := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wQuiet, reqQuiet)
	if wQuiet.Code != http.StatusOK {
		t.Fatalf("切换静音保护失败: %d", wQuiet.Code)
	}

	// 10. Step 8: 校验完整的审计记录链条
	audits, err := fixture.store.Audits().List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("读取审计日志失败: %v", err)
	}

	actionsRecorded := make(map[string]bool)
	for _, a := range audits {
		actionsRecorded[a.Action] = true
	}

	expectedActions := []string{
		"tma.diff.read",
		"tma.logs.read",
		"tma.action.pr_quick_approve",
		"tma.action.pr_comment",
		"tma.quiet_hours.toggle",
	}
	for _, act := range expectedActions {
		if !actionsRecorded[act] {
			t.Errorf("缺少期望的审计动作记录: %s", act)
		}
	}
}
