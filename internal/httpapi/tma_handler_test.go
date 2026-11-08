package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

const testTMABotToken = "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ_test_token"

func setupTMAServer(t *testing.T, tmaEnabled bool, allowedUser TelegramUser, customExec ChatOpsActionExecutor, customGH *githubx.AppClient, customLogger ...*slog.Logger) (*httpTestFixture, string) {
	t.Helper()
	var log *slog.Logger
	if len(customLogger) > 0 {
		log = customLogger[0]
	}
	testRing := setupTestKeyRing(t)
	fixture := newHTTPTestFixture(t, httpTestOptions{
		tmaEnabled:      tmaEnabled,
		publicBaseURL:   "https://reposentinel.example",
		chatOpsExecutor: customExec,
		githubClient:    customGH,
		logger:          log,
		keyRing:         &testRing,
	})

	ctx := t.Context()
	// 配置 Telegram Bot Token 与白名单至 store.Settings
	_, err1 := fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops.telegram.bot_token",
		ValueJSON: []byte(strconv.Quote(testTMABotToken)),
	})
	_, err2 := fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(strconv.Quote(strconv.FormatInt(allowedUser.ID, 10))),
	})
	if err1 != nil || err2 != nil {
		t.Fatalf("Upsert error err1=%v, err2=%v", err1, err2)
	}
	InvalidateResolvedBotTokenCache()

	initData := createTestInitData(testTMABotToken, allowedUser, time.Now().Unix(), false)
	return fixture, initData
}

// TestTMAHandler_FeatureFlagDisabled 验证断言 9：
// 当 cfg.HTTP.TMA.Enabled 为 false 时，所有 /api/v1/tma/* 路由严格返回 404
func TestTMAHandler_FeatureFlagDisabled(t *testing.T) {
	user := TelegramUser{ID: 88801, Username: "tma_tester"}
	fixture, initData := setupTMAServer(t, false, user, nil, nil)

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/tma/dashboard", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-123", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-123/diff", ""},
		{http.MethodGet, "/api/v1/tma/events/ev-123/logs", ""},
		{http.MethodPost, "/api/v1/tma/actions", `{"action":"workflow_rerun","event_id":"ev-123"}`},
		{http.MethodPost, "/api/v1/tma/quiet-hours", `{"enabled":true}`},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var bodyReader io.Reader
			if ep.body != "" {
				bodyReader = strings.NewReader(ep.body)
			}
			req := httptest.NewRequest(ep.method, ep.path, bodyReader)
			req.Header.Set("X-Telegram-Init-Data", initData)
			req.Header.Set("Origin", "https://reposentinel.example")
			if ep.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("特性开关关闭时，期望 404 Not Found, 实际: %d, body: %s", w.Code, w.Body.String())
			}
			var errResp apiErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if errResp.ErrorCode != errorCodeNotFound {
				t.Fatalf("期望错误码 not_found, 实际: %s", errResp.ErrorCode)
			}
		})
	}
}

// TestTMAHandler_RateLimitingAndOrigin 验证断言 12 & 13：
// 跨站未授权 Origin 拦截 403；限流超限返回 429 带 Retry-After: 60
func TestTMAHandler_RateLimitingAndOrigin(t *testing.T) {
	user := TelegramUser{ID: 88802, Username: "rate_tester"}
	fixture, initData := setupTMAServer(t, true, user, nil, nil)

	t.Run("断言12_未授权Origin拦截403", func(t *testing.T) {
		badOrigins := []string{
			"https://evil.attacker.com",
			"http://reposentinel.example",       // 协议不符
			"https://reposentinel.example:8443", // 端口不符
			"null",
		}
		for _, bo := range badOrigins {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
			req.Header.Set("X-Telegram-Init-Data", initData)
			req.Header.Set("Origin", bo)
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("未授权 Origin %q 期望 403 Forbidden, 实际: %d", bo, w.Code)
			}
			var errResp apiErrorResponse
			_ = json.Unmarshal(w.Body.Bytes(), &errResp)
			if errResp.ErrorCode != errorCodeForbidden {
				t.Fatalf("期望错误码 forbidden, 实际: %s", errResp.ErrorCode)
			}
		}
	})

	t.Run("断言12_合法Origin或无Origin放行", func(t *testing.T) {
		// 合法 Origin
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Origin", "https://reposentinel.example")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("合法 Origin 期望 200 OK, 实际: %d, body: %s", w.Code, w.Body.String())
		}

		// 无 Origin 头（同源相对路径调用）
		reqNoOrigin := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		reqNoOrigin.Header.Set("X-Telegram-Init-Data", initData)
		wNoOrigin := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wNoOrigin, reqNoOrigin)
		if wNoOrigin.Code != http.StatusOK {
			t.Fatalf("无 Origin 请求期望 200 OK, 实际: %d", wNoOrigin.Code)
		}
	})

	t.Run("断言13_写操作10次限流与Retry-After", func(t *testing.T) {
		// 注册一个启用的 telegram 渠道供 quiet-hours 测试
		_, _ = fixture.store.Channels().Upsert(t.Context(), store.NotificationChannel{
			ID:          "ch-tg-rl",
			ChannelType: store.ChannelTelegram,
			Name:        "Telegram Channel",
			Enabled:     true,
		})

		writeUser := TelegramUser{ID: 88803, Username: "write_limiter"}
		writeInitData := createTestInitData(testTMABotToken, writeUser, time.Now().Unix(), false)
		_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
			Key:       "chatops.telegram.allowed_user_ids",
			ValueJSON: []byte(strconv.Quote("88802,88803")),
		})

		// 连续调用 10 次 POST 写操作
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
			req.Header.Set("X-Telegram-Init-Data", writeInitData)
			req.Header.Set("Origin", "https://reposentinel.example")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("第 %d 次写操作期望 200, 实际: %d, body: %s", i+1, w.Code, w.Body.String())
			}
		}

		// 第 11 次写操作必须返回 429
		reqOver := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
		reqOver.Header.Set("X-Telegram-Init-Data", writeInitData)
		reqOver.Header.Set("Origin", "https://reposentinel.example")
		reqOver.Header.Set("Content-Type", "application/json")
		wOver := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wOver, reqOver)

		if wOver.Code != http.StatusTooManyRequests {
			t.Fatalf("超出写限额期望 429 Too Many Requests, 实际: %d, body: %s", wOver.Code, wOver.Body.String())
		}
		retryAfter := wOver.Header().Get("Retry-After")
		if retryAfter != "60" {
			t.Fatalf("期望 Retry-After: 60, 实际: %q", retryAfter)
		}
		var errResp apiErrorResponse
		_ = json.Unmarshal(wOver.Body.Bytes(), &errResp)
		if errResp.ErrorCode != errorCodeRateLimited {
			t.Fatalf("期望错误码 rate_limited, 实际: %s", errResp.ErrorCode)
		}
	})

	t.Run("断言13_读操作60次限流与Retry-After", func(t *testing.T) {
		readUser := TelegramUser{ID: 88804, Username: "read_limiter"}
		readInitData := createTestInitData(testTMABotToken, readUser, time.Now().Unix(), false)
		_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
			Key:       "chatops.telegram.allowed_user_ids",
			ValueJSON: []byte(strconv.Quote("88802,88803,88804")),
		})

		// 连续调用 60 次 GET 读操作
		for i := 0; i < 60; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
			req.Header.Set("X-Telegram-Init-Data", readInitData)
			req.Header.Set("Origin", "https://reposentinel.example")
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("第 %d 次读操作期望 200, 实际: %d, body: %s", i+1, w.Code, w.Body.String())
			}
		}

		// 第 61 次读操作必须返回 429
		reqOver := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
		reqOver.Header.Set("X-Telegram-Init-Data", readInitData)
		reqOver.Header.Set("Origin", "https://reposentinel.example")
		wOver := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wOver, reqOver)

		if wOver.Code != http.StatusTooManyRequests {
			t.Fatalf("超出读限额期望 429 Too Many Requests, 实际: %d, body: %s", wOver.Code, wOver.Body.String())
		}
		retryAfter := wOver.Header().Get("Retry-After")
		if retryAfter != "60" {
			t.Fatalf("期望 Retry-After: 60, 实际: %q", retryAfter)
		}
	})
}

type mockTMAActionExecutor struct {
	mu          sync.Mutex
	actions     []chatops.ActionData
	outcomeFunc func(action chatops.ActionData) (*chatops.Outcome, error)
}

func (m *mockTMAActionExecutor) Execute(ctx context.Context, action chatops.ActionData) error {
	m.mu.Lock()
	m.actions = append(m.actions, action)
	m.mu.Unlock()
	return nil
}

func (m *mockTMAActionExecutor) ExecuteWithOutcome(ctx context.Context, action chatops.ActionData) (*chatops.Outcome, error) {
	m.mu.Lock()
	m.actions = append(m.actions, action)
	fn := m.outcomeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(action)
	}
	return &chatops.Outcome{
		Status:  chatops.StatusExecuted,
		Action:  action.Action,
		Message: "success",
	}, nil
}

// TestTMAHandler_DirectIntentValidationAndCrossRepo 验证断言 5 & 6：
// 忽略客户端传入的 owner/repo/run_id；跨仓库或未纳管仓库一律 403
func TestTMAHandler_DirectIntentValidationAndCrossRepo(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88805, Username: "intent_tester"}
	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAServer(t, true, user, mockExec, nil)

	// 1. 创建合法已纳管仓库
	monitoredRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-monitored-1",
		Owner:          "sentinel-org",
		Name:           "valid-repo",
		FullName:       "sentinel-org/valid-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建纳管仓库失败: %v", err)
	}

	// 2. 创建未启用监控的仓库
	unmonitoredRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:       "repo-unmonitored-2",
		Owner:    "sentinel-org",
		Name:     "unmonitored-repo",
		FullName: "sentinel-org/unmonitored-repo",
	})
	if err != nil {
		t.Fatalf("创建未纳管仓库失败: %v", err)
	}
	monitorDisabled := false
	_ = fixture.store.Repositories().UpdateSettings(ctx, unmonitoredRepo.ID, store.RepositorySettings{
		MonitorEnabled: &monitorDisabled,
	})

	// 3. 创建已归档仓库
	archivedRepo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-archived-3",
		Owner:          "sentinel-org",
		Name:           "archived-repo",
		FullName:       "sentinel-org/archived-repo",
		MonitorEnabled: true,
		IsArchived:     true,
	})
	if err != nil {
		t.Fatalf("创建归档仓库失败: %v", err)
	}

	// 4. 创建关联事件
	subjectPR := int64(42)
	runID := int64(9901)
	validEvent, err := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-valid-1",
		DedupeFingerprint: "fp-valid-1",
		RepositoryID:      &monitoredRepo.ID,
		SubjectNumber:     &subjectPR,
		WorkflowRunID:     &runID,
		Kind:              "pull_request",
		Title:             "PR 42",
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建合法事件失败: %v", err)
	}

	unmonitoredEvent, unmonErr := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-unmonitored-2",
		DedupeFingerprint: "fp-unmonitored-2",
		RepositoryID:      &unmonitoredRepo.ID,
		SubjectNumber:     &subjectPR,
		Kind:              "pull_request",
		Title:             "Unmonitored PR",
		OccurredAt:        time.Now().UTC(),
	})
	if unmonErr != nil {
		t.Fatalf("create unmonitored event error: %v", unmonErr)
	}

	archivedEvent, archErr := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-archived-3",
		DedupeFingerprint: "fp-archived-3",
		RepositoryID:      &archivedRepo.ID,
		SubjectNumber:     &subjectPR,
		Kind:              "pull_request",
		Title:             "Archived PR",
		OccurredAt:        time.Now().UTC(),
	})
	if archErr != nil {
		t.Fatalf("create archived event error: %v", archErr)
	}

	t.Run("断言6_跨仓库或未纳管仓库一律403", func(t *testing.T) {
		// 查询未纳管仓库的事件详情 -> 403
		req1 := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+unmonitoredEvent.ID, nil)
		req1.Header.Set("X-Telegram-Init-Data", initData)
		w1 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w1, req1)
		if w1.Code != http.StatusForbidden {
			t.Fatalf("未纳管仓库事件期望 403, 实际: %d", w1.Code)
		}

		// 查询归档仓库的事件详情 -> 403
		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+archivedEvent.ID, nil)
		req2.Header.Set("X-Telegram-Init-Data", initData)
		w2 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w2, req2)
		if w2.Code != http.StatusForbidden {
			t.Fatalf("归档仓库事件期望 403, 实际: %d", w2.Code)
		}

		// 对未纳管仓库的事件发起 action -> 403
		actionPayload := fmt.Sprintf(`{"action":"pr_quick_approve","event_id":"%s"}`, unmonitoredEvent.ID)
		req3 := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(actionPayload))
		req3.Header.Set("X-Telegram-Init-Data", initData)
		req3.Header.Set("Content-Type", "application/json")
		w3 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w3, req3)
		if w3.Code != http.StatusForbidden {
			t.Fatalf("对未纳管仓库操作期望 403, 实际: %d", w3.Code)
		}

		// 不存在的 event_id -> 404
		req4 := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/non-existent-id", nil)
		req4.Header.Set("X-Telegram-Init-Data", initData)
		w4 := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w4, req4)
		if w4.Code != http.StatusNotFound {
			t.Fatalf("不存在的事件期望 404, 实际: %d", w4.Code)
		}
	})

	t.Run("断言5_DirectIntent忽略客户端伪造的owner_repo_run_id", func(t *testing.T) {
		mockExec.mu.Lock()
		mockExec.actions = nil
		mockExec.mu.Unlock()

		// 客户端恶意附带 fake 字段
		fakePayload := map[string]any{
			"event_id":     validEvent.ID,
			"action":       "pr_quick_approve",
			"owner":        "evil-attacker",
			"repo":         "fake-target-repo",
			"issue_number": 999999,
			"run_id":       "8888888",
		}
		bodyBytes, _ := json.Marshal(fakePayload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", bytes.NewReader(bodyBytes))
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("合法 event_id 期望 200 OK, 实际: %d, body: %s", w.Code, w.Body.String())
		}

		// 验证真正被执行的 ActionData
		mockExec.mu.Lock()
		defer mockExec.mu.Unlock()
		if len(mockExec.actions) != 1 {
			t.Fatalf("期望执行 1 次动作, 实际: %d", len(mockExec.actions))
		}
		executed := mockExec.actions[0]
		if executed.RepoID != monitoredRepo.ID {
			t.Fatalf("期望解析库中真实 RepoID %q, 实际被伪造为: %q", monitoredRepo.ID, executed.RepoID)
		}
		if executed.IssueNumber != int(subjectPR) {
			t.Fatalf("期望解析库中真实 PR #%d, 实际为: %d", subjectPR, executed.IssueNumber)
		}
		if executed.ActorID != strconv.FormatInt(user.ID, 10) {
			t.Fatalf("期望 ActorID 为 Telegram 用户 ID %d, 实际: %s", user.ID, executed.ActorID)
		}
	})
}

// TestTMAHandler_ActionsConcurrencyAndIdempotency 验证断言 7：
// 首个请求执行中并发第二请求命中 ReceiptPending 返回 HTTP 409 Conflict 与标准错误体 errorCodeConflict；
// 已完成动作重复请求返回 HTTP 200 与 StatusAlreadyDone；
// 不同正文 PR 评论携带不同哈希键各自成功执行。
func TestTMAHandler_ActionsConcurrencyAndIdempotency(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88806, Username: "concurrency_tester"}
	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAServer(t, true, user, mockExec, nil)

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-action-1",
		Owner:          "sentinel-org",
		Name:           "test-repo",
		FullName:       "sentinel-org/test-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	prNum := int64(10)
	evPR, err := fixture.store.Events().Create(ctx, store.Event{
		ID:            "ev-action-pr-1",
		RepositoryID:  &repo.ID,
		SubjectNumber: &prNum,
		Kind:          "pull_request",
		Title:         "PR 10",
		OccurredAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建 PR 事件失败: %v", err)
	}

	t.Run("断言7_并发处理中命中StatusPending返回409Conflict与标准错误体", func(t *testing.T) {
		mockExec.mu.Lock()
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			return &chatops.Outcome{
				Status:  chatops.StatusPending,
				Action:  action.Action,
				Message: "该动作已在后台处理中，请勿重复操作",
			}, nil
		}
		mockExec.mu.Unlock()

		body := fmt.Sprintf(`{"action":"pr_quick_approve","event_id":"%s"}`, evPR.ID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(body))
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("并发抢占中期望 HTTP 409 Conflict, 实际: %d, body: %s", w.Code, w.Body.String())
		}

		var errResp apiErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("解析 409 响应体失败: %v", err)
		}
		if errResp.ErrorCode != errorCodeConflict {
			t.Fatalf("期望 error_code 为 conflict, 实际: %s", errResp.ErrorCode)
		}
		if errResp.Details["reason"] != "action_in_progress" {
			t.Fatalf("期望 details.reason 为 action_in_progress, 实际: %v", errResp.Details["reason"])
		}
		if errResp.Details["action"] != "pr_quick_approve" {
			t.Fatalf("期望 details.action 为 pr_quick_approve, 实际: %v", errResp.Details["action"])
		}
	})

	t.Run("断言7_已完成动作重复提交返回200OK且包含StatusAlreadyDone", func(t *testing.T) {
		mockExec.mu.Lock()
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			return &chatops.Outcome{
				Status:     chatops.StatusAlreadyDone,
				Action:     action.Action,
				Message:    "操作此前已执行完成，跳过重复操作",
				ExternalID: "ext-12345",
			}, nil
		}
		mockExec.mu.Unlock()

		body := fmt.Sprintf(`{"action":"pr_quick_approve","event_id":"%s"}`, evPR.ID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(body))
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("重复提交期望 200 OK, 实际: %d, body: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "already_done" {
			t.Fatalf("期望 status 为 already_done, 实际: %v", resp["status"])
		}
		if resp["external_id"] != "ext-12345" {
			t.Fatalf("期望 external_id 为 ext-12345, 实际: %v", resp["external_id"])
		}
	})

	t.Run("断言7_PR评论内容哈希隔离与相同内容拦截", func(t *testing.T) {
		mockExec.mu.Lock()
		mockExec.actions = nil
		mockExec.outcomeFunc = func(action chatops.ActionData) (*chatops.Outcome, error) {
			return &chatops.Outcome{
				Status:  chatops.StatusExecuted,
				Action:  action.Action,
				Message: "评论发表成功",
			}, nil
		}
		mockExec.mu.Unlock()

		// 评论 A
		bodyA := fmt.Sprintf(`{"action":"pr_comment","event_id":"%s","comment_body":"LGTM! Looks good to merge."}`, evPR.ID)
		reqA := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(bodyA))
		reqA.Header.Set("X-Telegram-Init-Data", initData)
		reqA.Header.Set("Content-Type", "application/json")
		wA := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wA, reqA)
		if wA.Code != http.StatusOK {
			t.Fatalf("评论 A 提交失败: %d, body: %s", wA.Code, wA.Body.String())
		}

		// 评论 B（不同正文）
		bodyB := fmt.Sprintf(`{"action":"pr_comment","event_id":"%s","comment_body":"Please fix the lint errors."}`, evPR.ID)
		reqB := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(bodyB))
		reqB.Header.Set("X-Telegram-Init-Data", initData)
		reqB.Header.Set("Content-Type", "application/json")
		wB := httptest.NewRecorder()
		fixture.handler.ServeHTTP(wB, reqB)
		if wB.Code != http.StatusOK {
			t.Fatalf("评论 B 提交失败: %d, body: %s", wB.Code, wB.Body.String())
		}

		mockExec.mu.Lock()
		defer mockExec.mu.Unlock()
		if len(mockExec.actions) != 2 {
			t.Fatalf("期望两次动作均执行, 实际: %d", len(mockExec.actions))
		}
		keyA := mockExec.actions[0].ID
		keyB := mockExec.actions[1].ID
		if keyA == keyB {
			t.Fatalf("不同正文评论幂等键不应相同: keyA=%s, keyB=%s", keyA, keyB)
		}
		expectedPrefix := fmt.Sprintf("tma:comment:%d:%s:10:", user.ID, repo.ID)
		if !strings.HasPrefix(keyA, expectedPrefix) {
			t.Fatalf("幂等键 A 格式不符: %s", keyA)
		}
		if !strings.HasPrefix(keyB, expectedPrefix) {
			t.Fatalf("幂等键 B 格式不符: %s", keyB)
		}
	})
}

// TestTMAHandler_LogsStreamTruncation 验证断言 8：
// 超 2 MiB / 200 行日志经 io.LimitReader 截断，保留尾部 200 行，响应 truncated: true
func TestTMAHandler_LogsStreamTruncation(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88807, Username: "log_tester"}

	var servedLogContent string
	var servedJobs []githubx.WorkflowJobItem

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/jobs") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": servedJobs,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/actions/jobs/") && strings.HasSuffix(r.URL.Path, "/logs") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(servedLogContent))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	fixture, initData := setupTMAServer(t, true, user, nil, ghClient)

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-log-1",
		Owner:          "sentinel-org",
		Name:           "log-repo",
		FullName:       "sentinel-org/log-repo",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}

	runID := int64(7788)
	evLog, err := fixture.store.Events().Create(ctx, store.Event{
		ID:            "ev-log-1",
		RepositoryID:  &repo.ID,
		WorkflowRunID: &runID,
		Kind:          "workflow_run",
		Title:         "CI run #7788",
		OccurredAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建事件失败: %v", err)
	}

	t.Run("断言8_超过200行日志截断保留尾部200行并标记truncated", func(t *testing.T) {
		servedJobs = []githubx.WorkflowJobItem{
			{ID: 101, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 102, Name: "test", Status: "completed", Conclusion: "failure"},
		}

		// 构造 300 行日志
		var logLines []string
		for i := 1; i <= 300; i++ {
			logLines = append(logLines, fmt.Sprintf("2026-11-08T00:00:%02dZ Line %03d: log message detail", i%60, i))
		}
		servedLogContent = strings.Join(logLines, "\n") + "\n"

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+evLog.ID+"/logs", nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("获取日志失败: %d, body: %s", w.Code, w.Body.String())
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
			t.Fatal("超过 200 行日志期望 truncated=true, 实际为 false")
		}
		if resp.JobID != 102 || resp.JobName != "test" {
			t.Fatalf("期望优先定位失败的 Job (id: 102, name: test), 实际: id=%d, name=%s", resp.JobID, resp.JobName)
		}

		lines := strings.Split(resp.Logs, "\n")
		if len(lines) != 200 {
			t.Fatalf("截断后期望保留尾部 200 行, 实际行数: %d", len(lines))
		}
		if !strings.Contains(lines[0], "Line 101:") {
			t.Fatalf("截断首行期望包含 Line 101, 实际: %s", lines[0])
		}
		if !strings.Contains(lines[199], "Line 300:") {
			t.Fatalf("截断末行期望包含 Line 300, 实际: %s", lines[199])
		}
	})

	t.Run("断言8_未超限日志不截断", func(t *testing.T) {
		servedJobs = []githubx.WorkflowJobItem{
			{ID: 201, Name: "lint", Status: "completed", Conclusion: "failure"},
		}
		smallLines := []string{"log step 1", "log step 2", "log step 3: failure"}
		servedLogContent = strings.Join(smallLines, "\n")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+evLog.ID+"/logs", nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("获取日志失败: %d", w.Code)
		}
		var resp struct {
			Logs      string `json:"logs"`
			Truncated bool   `json:"truncated"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Truncated {
			t.Fatal("未超限日志期望 truncated=false, 实际为 true")
		}
		if resp.Logs != servedLogContent {
			t.Fatalf("期望完整日志内容, 实际: %q", resp.Logs)
		}
	})

	t.Run("断言8_超过2MiB超大日志流式截断", func(t *testing.T) {
		servedJobs = []githubx.WorkflowJobItem{
			{ID: 301, Name: "large-run", Status: "completed", Conclusion: "failure"},
		}
		// 生成 2.5 MiB 的大日志
		hugeLog := strings.Repeat("A long log line with lots of information 1234567890\n", 50000)
		servedLogContent = hugeLog

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+evLog.ID+"/logs", nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("获取大日志失败: %d", w.Code)
		}
		var resp struct {
			Logs      string `json:"logs"`
			Truncated bool   `json:"truncated"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !resp.Truncated {
			t.Fatal("超 2 MiB 日志期望 truncated=true, 实际为 false")
		}
	})
}

// TestTMAHandler_DiffRead 验证 PR Diff 拉取与审计留痕
func TestTMAHandler_DiffRead(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88808, Username: "diff_tester"}

	const expectedDiff = "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pulls/15") {
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			_, _ = w.Write([]byte(expectedDiff))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	fixture, initData := setupTMAServer(t, true, user, nil, ghClient)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-diff-1",
		Owner:          "sentinel-org",
		Name:           "diff-repo",
		FullName:       "sentinel-org/diff-repo",
		MonitorEnabled: true,
	})

	prNum := int64(15)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:            "ev-diff-1",
		RepositoryID:  &repo.ID,
		SubjectNumber: &prNum,
		Kind:          "pull_request",
		Title:         "PR 15 Diff Test",
		OccurredAt:    time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/"+ev.ID+"/diff", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("拉取 Diff 失败: %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Diff string `json:"diff"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Diff != expectedDiff {
		t.Fatalf("期望 Diff 文本 %q, 实际: %q", expectedDiff, resp.Diff)
	}

	// 检查审计
	audits, err := fixture.store.Audits().List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("查询审计日志失败: %v", err)
	}
	found := false
	for _, a := range audits {
		if a.Action == "tma.diff.read" && a.ActorType == "telegram/tma" && a.ActorID == "88808" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("未找到 tma.diff.read 审计日志")
	}
}

// TestTMAHandler_QuietHoursToggle 验证夜间静音开关原子变更与审计留痕
func TestTMAHandler_QuietHoursToggle(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88809, Username: "qh_tester"}
	fixture, initData := setupTMAServer(t, true, user, nil, nil)

	encTokenToggle, _ := setupTestKeyRing(t).Encrypt(ctx, []byte(testTMABotToken), []byte(notify.AAD))
	ch, err := fixture.store.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                "ch-tg-toggle-1",
		ChannelType:       store.ChannelTelegram,
		Name:              "Telegram Alert",
		Enabled:           true,
		SecretEnvelope:    encTokenToggle,
		QuietHoursEnabled: false,
		QuietHoursStart:   "23:00",
		QuietHoursEnd:     "08:00",
		QuietHoursTZ:      "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}

	// 开启静音
	reqEnable := httptest.NewRequest(http.MethodPost, "/api/v1/tma/quiet-hours", strings.NewReader(`{"enabled":true}`))
	reqEnable.Header.Set("X-Telegram-Init-Data", initData)
	reqEnable.Header.Set("Content-Type", "application/json")
	wEnable := httptest.NewRecorder()
	fixture.handler.ServeHTTP(wEnable, reqEnable)

	if wEnable.Code != http.StatusOK {
		t.Fatalf("开启静音失败: %d, body: %s", wEnable.Code, wEnable.Body.String())
	}

	updated, err := fixture.store.Channels().Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("获取渠道失败: %v", err)
	}
	if !updated.QuietHoursEnabled {
		t.Fatal("渠道 QuietHoursEnabled 未被成功置为 true")
	}

	// 检查审计
	audits, _ := fixture.store.Audits().List(ctx, 10, 0)
	found := false
	for _, a := range audits {
		if a.Action == "tma.quiet_hours.toggle" && a.ActorType == "telegram/tma" && a.ActorID == "88809" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("未找到 tma.quiet_hours.toggle 审计日志")
	}
}

// TestTMAHandler_Dashboard 验证仪表盘数据聚合统计与未处理工作项
func TestTMAHandler_Dashboard(t *testing.T) {
	ctx := t.Context()
	user := TelegramUser{ID: 88811, Username: "dashboard_tester"}
	fixture, initData := setupTMAServer(t, true, user, nil, nil)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-dash-1",
		Owner:          "sentinel-org",
		Name:           "dash-repo",
		FullName:       "sentinel-org/dash-repo",
		SyncStatus:     store.SyncStatusActive,
		MonitorEnabled: true,
	})

	// 写入活跃工作项
	_, _, _ = fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:           "wi-open-1",
		RepositoryID: repo.ID,
		Number:       101,
		Kind:         "issue",
		State:        "open",
		Title:        "Bug in login",
		Ignored:      false,
	}, nil)

	// 写入已忽略工作项
	_, _, _ = fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:           "wi-ignored-2",
		RepositoryID: repo.ID,
		Number:       102,
		Kind:         "issue",
		State:        "open",
		Title:        "Ignored issue",
	}, nil)
	_ = fixture.store.WorkItems().SetIgnored(ctx, "wi-ignored-2", true)

	// 写入 Webhook Deliveries
	_, _ = fixture.store.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		DeliveryID: "del-success-1",
		EventType:  "issues",
		Status:     store.DeliveryProcessed,
		Payload:    []byte(`{}`),
	})
	_, _ = fixture.store.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		DeliveryID: "del-failed-2",
		EventType:  "issues",
		Status:     store.DeliveryFailed,
		Payload:    []byte(`{}`),
	})

	// 写入 Telegram 渠道
	encTokenDash, _ := setupTestKeyRing(t).Encrypt(ctx, []byte(testTMABotToken), []byte(notify.AAD))
	_, _ = fixture.store.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                "ch-tg-dash",
		ChannelType:       store.ChannelTelegram,
		Name:              "Telegram Alerts",
		Enabled:           true,
		SecretEnvelope:    encTokenDash,
		QuietHoursEnabled: true,
		QuietHoursStart:   "22:00",
		QuietHoursEnd:     "07:00",
		QuietHoursTZ:      "Asia/Shanghai",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("获取仪表盘数据失败: %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		MonitoredReposCount      int              `json:"monitored_repos_count"`
		PendingWorkItemsCount    int              `json:"pending_work_items_count"`
		WebhookDeliveriesTotal   int              `json:"webhook_deliveries_total"`
		WebhookDeliveriesSuccess int              `json:"webhook_deliveries_success"`
		WebhookDeliveryRate      float64          `json:"webhook_delivery_rate"`
		QuietHoursEnabled        bool             `json:"quiet_hours_enabled"`
		QuietHoursStart          string           `json:"quiet_hours_start"`
		QuietHoursEnd            string           `json:"quiet_hours_end"`
		QuietHoursTZ             string           `json:"quiet_hours_tz"`
		WorkItems                []store.WorkItem `json:"work_items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析仪表盘响应失败: %v", err)
	}

	if resp.MonitoredReposCount != 1 {
		t.Fatalf("期望纳管仓库数 1, 实际: %d", resp.MonitoredReposCount)
	}
	if resp.PendingWorkItemsCount != 1 {
		t.Fatalf("期望待办工作项数 1, 实际: %d", resp.PendingWorkItemsCount)
	}
	if resp.WebhookDeliveriesTotal != 2 {
		t.Fatalf("期望 Webhook 总数 2, 实际: %d", resp.WebhookDeliveriesTotal)
	}
	if resp.WebhookDeliveriesSuccess != 1 {
		t.Fatalf("期望 Webhook 成功数 1, 实际: %d", resp.WebhookDeliveriesSuccess)
	}
	if resp.WebhookDeliveryRate != 0.5 {
		t.Fatalf("期望 Webhook 成功率 0.5, 实际: %f", resp.WebhookDeliveryRate)
	}
	if !resp.QuietHoursEnabled || resp.QuietHoursStart != "22:00" || resp.QuietHoursEnd != "07:00" {
		t.Fatalf("期望夜间静音已开启 (22:00-07:00), 实际: enabled=%v, start=%s, end=%s",
			resp.QuietHoursEnabled, resp.QuietHoursStart, resp.QuietHoursEnd)
	}
	if len(resp.WorkItems) != 1 || resp.WorkItems[0].ID != "wi-open-1" {
		t.Fatalf("期望仅包含未忽略的活跃工作项 (wi-open-1), 实际: %+v", resp.WorkItems)
	}
}

// TestTMAHandler_ZeroSensitiveTokensInLogs 验证断言 11：
// Bot Token 与完整 initData 绝不出现在任何系统日志或响应体中
func TestTMAHandler_ZeroSensitiveTokensInLogs(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	user := TelegramUser{ID: 88810, Username: "token_leak_tester"}
	fixture, initData := setupTMAServer(t, true, user, nil, nil, logger)

	// 触发多个端点（包含成功、404、400、403、429 等）
	testRequests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/tma/dashboard", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/tma/events/not-found", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", strings.NewReader(`{"action":"invalid_action"}`)),
	}

	var allRespBodies string
	for _, req := range testRequests {
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)
		allRespBodies += w.Body.String() + "\n"
	}

	allLogs := logBuf.String()
	if strings.Contains(allLogs, testTMABotToken) {
		t.Fatalf("日志中泄漏了 Bot Token: %s", allLogs)
	}
	if strings.Contains(allRespBodies, testTMABotToken) {
		t.Fatalf("HTTP 响应体中泄漏了 Bot Token: %s", allRespBodies)
	}
}

func TestTMA_A6_WriteActionFailureAndRejectionAudit(t *testing.T) {
	// A6【P1】：TMA 写操作在失败与被拒绝（403/409/5xx）时也写审计，字段与 ChatOps 审计同源
	ctx := t.Context()
	user := TelegramUser{ID: 10001, Username: "operator_alex"}
	mockExec := &mockTMAActionExecutor{}
	fixture, initData := setupTMAServer(t, true, user, mockExec, nil)

	// 1. 跨仓库/未监控仓库拒绝 403
	unmonitoredRepo, errRepo := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:       "repo-unmonitored-test",
		Owner:    "test-org",
		Name:     "repo-unmonitored",
		FullName: "test-org/repo-unmonitored",
	})
	if errRepo != nil {
		t.Fatalf("upsert repo error: %v", errRepo)
	}
	monitorDisabled := false
	_ = fixture.store.Repositories().UpdateSettings(ctx, unmonitoredRepo.ID, store.RepositorySettings{
		MonitorEnabled: &monitorDisabled,
	})

	runID := int64(12345)
	evUnmonitored, errEv := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-unmonitored-1",
		DedupeFingerprint: "fp-unmonitored-1",
		RepositoryID:      &unmonitoredRepo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		OccurredAt:        time.Now().UTC(),
	})
	if errEv != nil {
		t.Fatalf("create event error: %v", errEv)
	}

	bodyJSON, _ := json.Marshal(map[string]any{
		"action":   "workflow_rerun",
		"event_id": evUnmonitored.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", bytes.NewReader(bodyJSON))
	req.Header.Set("X-Telegram-Init-Data", initData)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("未监控仓库写操作期望 403 Forbidden，实际: %d", rec.Code)
	}

	audits, err := fixture.store.Audits().List(ctx, 10, 0)
	if err != nil || len(audits) == 0 {
		t.Fatalf("未查询到审计日志: %v", err)
	}
	var auditDenied store.AuditLog
	for _, a := range audits {
		if a.Action == "tma.action.workflow_rerun" {
			auditDenied = a
			break
		}
	}
	if auditDenied.ID == "" {
		t.Fatal("未记录 tma.action.workflow_rerun 拒绝审计")
	}
	if auditDenied.ActorType != "telegram/tma" || auditDenied.ActorID != "10001" {
		t.Fatalf("审计主体信息不符: type=%s, id=%s", auditDenied.ActorType, auditDenied.ActorID)
	}
	var metaDenied map[string]any
	_ = json.Unmarshal(auditDenied.MetadataJSON, &metaDenied)
	if metaDenied["action_result"] != "denied" {
		t.Fatalf("期望 action_result denied, 实际: %v", metaDenied["action_result"])
	}
}

// ============================================================================
// Group B 测试：接口契约与日志 (B1 - B4)
// ============================================================================

func TestTMA_B1_NoFailedJobReturnsStatusWithoutDownloadingLogs(t *testing.T) {
	// B1【P1】：当所有 Job 均成功时，返回 status="no_failed_job"，且绝不调用 DownloadWorkflowJobLogs
	ctx := t.Context()
	user := TelegramUser{ID: 10001, Username: "operator_alex"}

	var downloadLogsCalled bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/jobs") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []map[string]any{
					{
						"id":         101,
						"name":       "build-success",
						"conclusion": "success",
						"steps": []map[string]any{
							{"name": "checkout", "conclusion": "success"},
							{"name": "build", "conclusion": "success"},
						},
					},
					{
						"id":         102,
						"name":       "test-success",
						"conclusion": "success",
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/logs") {
			downloadLogsCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("some logs"))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	fixture, initData := setupTMAServer(t, true, user, nil, ghClient)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-b1-test",
		Owner:          "sentinel-org",
		Name:           "b1-repo",
		FullName:       "sentinel-org/b1-repo",
		MonitorEnabled: true,
	})
	runID := int64(123456)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-b1-1",
		DedupeFingerprint: "fp-b1-1",
		RepositoryID:      &repo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Successful Run",
		OccurredAt:        time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200 OK，实际: %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Logs      string `json:"logs"`
		Status    string `json:"status"`
		Truncated bool   `json:"truncated"`
		JobID     int64  `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if resp.Status != "no_failed_job" {
		t.Fatalf("期望 status 为 no_failed_job, 实际: %s", resp.Status)
	}
	if resp.Logs != "" {
		t.Fatalf("期望 logs 为空，实际: %s", resp.Logs)
	}
	if downloadLogsCalled {
		t.Fatal("全部成功时不应调用 DownloadWorkflowJobLogs，但外部 API 被调用了！")
	}
}

func TestTMA_B2_LogStreamingExtremeCases_3MB_SingleLine_And_10K_Lines(t *testing.T) {
	// B2【P1】：测试 3 MiB 超长单行与 1 万行小行极限流式读取，断言单行长度上限截断与 byte_truncated / line_truncated
	ctx := t.Context()
	user := TelegramUser{ID: 10001, Username: "operator_alex"}

	var servedLogs []byte
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(servedLogs)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []map[string]any{
					{"id": 201, "name": "fail-job", "conclusion": "failure"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ghClient := &githubx.AppClient{
		HTTP:    mockServer.Client(),
		BaseURL: mockServer.URL,
	}

	fixture, initData := setupTMAServer(t, true, user, nil, ghClient)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-b2-test",
		Owner:          "sentinel-org",
		Name:           "b2-repo",
		FullName:       "sentinel-org/b2-repo",
		MonitorEnabled: true,
	})
	runID := int64(654321)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-b2-1",
		DedupeFingerprint: "fp-b2-1",
		RepositoryID:      &repo.ID,
		WorkflowRunID:     &runID,
		Kind:              "workflow_run",
		Title:             "Large Run",
		OccurredAt:        time.Now().UTC(),
	})

	t.Run("极限1：3 MiB 超长单行流式安全截断", func(t *testing.T) {
		// 构造无任何换行的 3 MiB 连续单行
		servedLogs = bytes.Repeat([]byte("X"), 3*1024*1024)

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		rec := httptest.NewRecorder()
		fixture.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("期望状态码 200 OK，实际: %d", rec.Code)
		}

		var resp struct {
			Logs          string `json:"logs"`
			Truncated     bool   `json:"truncated"`
			ByteTruncated bool   `json:"byte_truncated"`
			LineTruncated bool   `json:"line_truncated"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}

		if !resp.Truncated || !resp.ByteTruncated {
			t.Fatalf("期望 Truncated=true 且 ByteTruncated=true, 实际: Truncated=%v, ByteTruncated=%v", resp.Truncated, resp.ByteTruncated)
		}
		// 单行暂存不超过 4096 字节
		if len(resp.Logs) > 4096 {
			t.Fatalf("单行长度未在内存中安全封顶 4096 字节，实际长度: %d", len(resp.Logs))
		}
	})

	t.Run("极限2：10000 行短行流式读取保留最后 200 行", func(t *testing.T) {
		var buf bytes.Buffer
		for i := 1; i <= 10000; i++ {
			buf.WriteString(fmt.Sprintf("line %05d\n", i))
		}
		servedLogs = buf.Bytes()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tma/events/%s/logs", ev.ID), nil)
		req.Header.Set("X-Telegram-Init-Data", initData)
		rec := httptest.NewRecorder()
		fixture.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("期望状态码 200 OK，实际: %d", rec.Code)
		}

		var resp struct {
			Logs          string `json:"logs"`
			Truncated     bool   `json:"truncated"`
			LineTruncated bool   `json:"line_truncated"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}

		if !resp.Truncated || !resp.LineTruncated {
			t.Fatalf("期望 Truncated=true 且 LineTruncated=true, 实际: Truncated=%v, LineTruncated=%v", resp.Truncated, resp.LineTruncated)
		}
		lines := strings.Split(resp.Logs, "\n")
		if len(lines) != 200 {
			t.Fatalf("期望返回 200 行，实际: %d", len(lines))
		}
		if !strings.HasPrefix(lines[len(lines)-1], "line 10000") {
			t.Fatalf("最后一行期望包含 line 10000, 实际: %s", lines[len(lines)-1])
		}
	})
}

func TestTMA_B3_Pending409DetailsContract(t *testing.T) {
	// B3【P1】：409 Conflict 必须在 details 稳定包含 status:"pending" 和 reason:"action_in_progress"
	ctx := t.Context()
	user := TelegramUser{ID: 10001, Username: "operator_alex"}
	mockExec := &mockTMAActionExecutor{
		outcomeFunc: func(action chatops.ActionData) (*chatops.Outcome, error) {
			return &chatops.Outcome{
				Status:  chatops.StatusPending,
				Action:  action.Action,
				Message: "action in progress",
			}, nil
		},
	}
	fixture, initData := setupTMAServer(t, true, user, mockExec, nil)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-b3-test",
		Owner:          "sentinel-org",
		Name:           "b3-repo",
		FullName:       "sentinel-org/b3-repo",
		MonitorEnabled: true,
	})
	prNum := int64(10)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-b3-1",
		DedupeFingerprint: "fp-b3-1",
		RepositoryID:      &repo.ID,
		SubjectNumber:     &prNum,
		Kind:              "pull_request",
		Title:             "PR 10",
		OccurredAt:        time.Now().UTC(),
	})

	bodyJSON, _ := json.Marshal(map[string]any{
		"action":   "pr_quick_approve",
		"event_id": ev.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", bytes.NewReader(bodyJSON))
	req.Header.Set("X-Telegram-Init-Data", initData)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fixture.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("期望状态码 409 Conflict，实际: %d", rec.Code)
	}

	var errResp apiErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("反序列化 409 响应失败: %v", err)
	}
	if errResp.ErrorCode != "conflict" {
		t.Fatalf("期望 error_code 为 conflict, 实际: %s", errResp.ErrorCode)
	}
	if errResp.Details["status"] != "pending" {
		t.Fatalf("期望 details.status 为 pending，实际: %v", errResp.Details["status"])
	}
	if errResp.Details["reason"] != "action_in_progress" {
		t.Fatalf("期望 details.reason 为 action_in_progress，实际: %v", errResp.Details["reason"])
	}
}

func TestTMA_B4_CommentIdempotencyIsolatedByActorID(t *testing.T) {
	// B4【P1】：不同白名单用户对同一 PR 提交相同评论内容，因 stableKey 包含 actorID，互不阻塞均能成功执行；
	// 同一用户重复提交相同内容则被拦截
	ctx := t.Context()
	user1 := TelegramUser{ID: 10001, Username: "operator_alex"}
	user2 := TelegramUser{ID: 20002, Username: "operator_bob"}

	var executedKeys []string
	var mu sync.Mutex
	mockExec := &mockTMAActionExecutor{
		outcomeFunc: func(action chatops.ActionData) (*chatops.Outcome, error) {
			mu.Lock()
			defer mu.Unlock()
			executedKeys = append(executedKeys, action.ID)
			return &chatops.Outcome{
				Status:     chatops.StatusExecuted,
				Action:     action.Action,
				ExternalID: "comment-1",
				Message:    "Comment posted",
			}, nil
		},
	}

	fixture, initData1 := setupTMAServer(t, true, user1, mockExec, nil)
	// 将 user2 也加入白名单
	_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(`"10001,20002"`),
	})
	initData2 := createTestInitData(testTMABotToken, user2, time.Now().Unix(), false)

	repo, _ := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-b4-test",
		Owner:          "sentinel-org",
		Name:           "b4-repo",
		FullName:       "sentinel-org/b4-repo",
		MonitorEnabled: true,
	})
	prNum := int64(99)
	ev, _ := fixture.store.Events().Create(ctx, store.Event{
		ID:                "ev-b4-1",
		DedupeFingerprint: "fp-b4-1",
		RepositoryID:      &repo.ID,
		SubjectNumber:     &prNum,
		Kind:              "pull_request",
		Title:             "PR 99",
		OccurredAt:        time.Now().UTC(),
	})

	postComment := func(initData string, comment string) int {
		body, _ := json.Marshal(map[string]any{
			"action":       "pr_comment",
			"event_id":     ev.ID,
			"comment_body": comment,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tma/actions", bytes.NewReader(body))
		req.Header.Set("X-Telegram-Init-Data", initData)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		fixture.handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// 1. 用户 1 首次提交评论
	code1 := postComment(initData1, "LGTM from review")
	if code1 != http.StatusOK {
		t.Fatalf("用户 1 首次提交评论期望 200 OK，实际: %d", code1)
	}

	// 2. 用户 2 提交相同评论内容 -> 期望同样成功 200 OK，不被用户 1 拦截
	code2 := postComment(initData2, "LGTM from review")
	if code2 != http.StatusOK {
		t.Fatalf("用户 2 首次提交相同评论期望 200 OK，实际: %d", code2)
	}

	mu.Lock()
	keysCount := len(executedKeys)
	key1 := executedKeys[0]
	key2 := executedKeys[1]
	mu.Unlock()

	if keysCount != 2 {
		t.Fatalf("期望执行 2 次独立动作，实际执行了 %d 次", keysCount)
	}
	if !strings.Contains(key1, "10001") || !strings.Contains(key2, "20002") {
		t.Fatalf("幂等键未包含 ActorID 导致隔离失效: key1=%s, key2=%s", key1, key2)
	}
	if key1 == key2 {
		t.Fatalf("不同用户的幂等键产生了碰撞: key1=%s, key2=%s", key1, key2)
	}
}
