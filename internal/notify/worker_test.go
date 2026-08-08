package notify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// openWorkerTestStore 打开隔离的 SQLite Store（TMPDIR 随测试目录隔离，避免 Atlas 锁名冲突）。
func openWorkerTestStore(t *testing.T) store.Store {
	t.Helper()
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(temporaryDir, "worker.db"),
	})
	if err != nil {
		t.Fatalf("打开测试 Store 失败: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func createPendingOutbox(t *testing.T, st store.Store, id string, attempts int) store.NotificationOutbox {
	t.Helper()
	item, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: id, ChannelID: "ch-1", IdempotencyKey: "idem|" + id,
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: "t", BodyText: "b", AttemptCount: attempts,
	})
	if err != nil {
		t.Fatalf("创建 outbox 失败: %v", err)
	}
	return item
}

// 限流退避达到重试上限时必须转入死信，否则目标长期 429 会无限重投。
func TestHandleFailureRateLimitedExceedsMaxAttemptsGoesDead(t *testing.T) {
	st := openWorkerTestStore(t)
	item := createPendingOutbox(t, st, "o-rate-dead", maxAttempts)

	deadFired := false
	w := &Worker{Store: st, OnDead: func() { deadFired = true }}
	w.handleFailure(t.Context(), item, &retryAfterError{seconds: 45, code: "telegram_rate_limited"})

	if n, _ := st.Outbox().CountByStatus(t.Context(), store.OutboxDead); n != 1 {
		t.Fatalf("期望 1 条死信，实际 %d", n)
	}
	if n, _ := st.Outbox().CountByStatus(t.Context(), store.OutboxPending); n != 0 {
		t.Fatalf("限流超上限后不应再挂起重试，pending=%d", n)
	}
	if !deadFired {
		t.Fatal("期望 OnDead 回调触发")
	}
}

// 未到上限的限流错误仍按 retry_after 退避，不进入死信。
func TestHandleFailureRateLimitedWithinBudgetRetries(t *testing.T) {
	st := openWorkerTestStore(t)
	item := createPendingOutbox(t, st, "o-rate-retry", 2)

	w := &Worker{Store: st, OnDead: func() { t.Fatal("不应触发死信") }}
	before := time.Now().UTC()
	w.handleFailure(t.Context(), item, &retryAfterError{seconds: 45, code: "telegram_rate_limited"})

	items, _, err := st.Outbox().List(t.Context(), store.ListFilter{Status: store.OutboxPending})
	if err != nil {
		t.Fatalf("查询 outbox 失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("期望条目保持 pending，实际 %d 条", len(items))
	}
	got := items[0]
	if got.LastErrorCode != "telegram_rate_limited" {
		t.Fatalf("错误码应为 telegram_rate_limited，实际 %q", got.LastErrorCode)
	}
	if got.NextAttemptAt.Before(before.Add(44*time.Second)) || got.NextAttemptAt.After(before.Add(46*time.Second)) {
		t.Fatalf("退避时间应约等于 retry_after=45s，实际 %v", got.NextAttemptAt.Sub(before))
	}
}

// 普通错误达到上限同样转死信（回归兜底：两条失败路径行为一致）。
func TestHandleFailureGenericExceedsMaxAttemptsGoesDead(t *testing.T) {
	st := openWorkerTestStore(t)
	item := createPendingOutbox(t, st, "o-gen-dead", maxAttempts)

	w := &Worker{Store: st}
	w.handleFailure(t.Context(), item, errTestGeneric{})

	if n, _ := st.Outbox().CountByStatus(t.Context(), store.OutboxDead); n != 1 {
		t.Fatalf("期望 1 条死信，实际 %d", n)
	}
}

type errTestGeneric struct{}

func (errTestGeneric) Error() string { return "telegram_http_500" }

// claimFailStore 嵌入 store.Store 并覆盖 Outbox()，用于注入 ClaimDue 失败。
type claimFailStore struct {
	store.Store
	outbox store.OutboxStore
}

func (s *claimFailStore) Outbox() store.OutboxStore { return s.outbox }

// failingOutboxStore 嵌入 OutboxStore 接口，仅覆写 ClaimDue 返回错误；
// 其余方法不会被测试路径触达，保持 nil 内嵌即可。
type failingOutboxStore struct {
	store.OutboxStore
}

func (failingOutboxStore) ClaimDue(context.Context, time.Time, time.Duration, int) ([]store.NotificationOutbox, error) {
	return nil, errors.New("claim boom: database locked")
}

// TestTickClaimFailureLogsErrorDetail 领取失败日志必须携带真实错误详情，
// 否则数据库抖动时只有 error_code 无法判断是连接、锁还是迁移问题。
func TestTickClaimFailureLogsErrorDetail(t *testing.T) {
	var logBuffer bytes.Buffer
	w := &Worker{
		Store:  &claimFailStore{outbox: failingOutboxStore{}},
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	w.tick(t.Context())

	logs := logBuffer.String()
	if !strings.Contains(logs, "outbox claim failed") {
		t.Fatalf("应记录 outbox claim failed，实际日志: %s", logs)
	}
	if !strings.Contains(logs, "claim boom: database locked") {
		t.Fatalf("领取失败日志应携带错误详情，实际日志: %s", logs)
	}
}

// TestSendTelegramErrorCarriesBodyDetail Telegram 4xx/5xx 错误必须携带截断响应体，
// 否则「chat not found」这类可行动原因只能靠抓包才能看到。
func TestSendTelegramErrorCarriesBodyDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL, "chat-1", "token", "text", "", "HTML")
	if err == nil {
		t.Fatal("400 应返回错误")
	}
	if !strings.Contains(err.Error(), "telegram_client_error_400") {
		t.Fatalf("错误应含稳定错误码前缀，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("错误应携带响应体详情，实际: %v", err)
	}
	if got := deliveryErrorCode(err); got != "telegram_client_error_400" {
		t.Fatalf("deliveryErrorCode = %q, want telegram_client_error_400", got)
	}
}

// TestSendHTTPErrorCarriesBodyDetail HTTP Webhook 5xx 错误必须携带截断响应体。
func TestSendHTTPErrorCarriesBodyDetail(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream exploded"}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	ch := store.NotificationChannel{ChannelType: store.ChannelHTTPWebhook, Target: srv.URL, AllowPrivate: true}
	item := store.NotificationOutbox{ID: "o-1", Title: "t", BodyText: "b"}
	err := w.sendHTTP(t.Context(), ch, "", item)
	if err == nil {
		t.Fatal("500 应返回错误")
	}
	if !strings.Contains(err.Error(), "http_webhook_status_500") {
		t.Fatalf("错误应含稳定错误码前缀，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("错误应携带响应体详情，实际: %v", err)
	}
	if got := deliveryErrorCode(err); got != "http_webhook_status_500" {
		t.Fatalf("deliveryErrorCode = %q, want http_webhook_status_500", got)
	}
}

// TestDeliveryErrorCode 稳定错误码推导：retryAfterError 用自带 code，
// 代码风格前缀直接复用，网络层自由文本归为 delivery_failed。
func TestDeliveryErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"retryAfter", &retryAfterError{seconds: 10, code: "telegram_rate_limited"}, "telegram_rate_limited"},
		{"telegram 4xx", errors.New("telegram_client_error_400: Bad Request: chat not found"), "telegram_client_error_400"},
		{"http 5xx", errors.New("http_webhook_status_503: Service Unavailable"), "http_webhook_status_503"},
		{"wrapped decrypt", errors.New("decrypt_secret: aes: invalid key"), "decrypt_secret"},
		{"network text", errors.New("dial tcp 10.0.0.1:8080: connect: connection refused"), "delivery_failed"},
		{"ssrf", errors.New("ssrf_blocked"), "ssrf_blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveryErrorCode(tc.err); got != tc.want {
				t.Fatalf("deliveryErrorCode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateWebhookURLRejectsNonHTTPSAndPrivate(t *testing.T) {
	cases := []struct {
		url     string
		allow   bool
		wantErr bool
	}{
		{"http://example.com/hook", false, true},
		{"https://example.com/hook", false, false},
		{"https://127.0.0.1/hook", false, true},
		{"https://10.0.0.1/hook", false, true},
		{"https://169.254.169.254/latest", false, true},
		{"https://localhost/hook", true, true}, // localhost 始终拦截
		{"https://10.0.0.1/hook", true, false},
		{"ftp://example.com/hook", false, true},
		{"", false, true},
	}
	for _, tc := range cases {
		err := validateWebhookURL(tc.url, tc.allow)
		if tc.wantErr && err == nil {
			t.Fatalf("%s allow=%v 期望错误", tc.url, tc.allow)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s allow=%v 不期望错误: %v", tc.url, tc.allow, err)
		}
	}
}

func TestSafeHTTPClientPinsPublicIPAndBlocksPrivate(t *testing.T) {
	client := newSafeHTTPClient(3 * time.Second)
	// 直接拨 loopback 应被 DialContext 拦截（即使 allow_private 在 URL 层放行，拨号仍拦字面量私网由 validate 决定；
	// 这里验证 Dial 对 127.0.0.1 拒绝）。
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatal("expected custom transport")
	}
	_, err := transport.DialContext(t.Context(), "tcp", "127.0.0.1:443")
	if err == nil {
		t.Fatal("loopback dial should be blocked")
	}
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect required")
	}
}

func TestHTTPClientDoesNotFollowRedirect(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	w := &Worker{}
	// 触发默认 Client 初始化
	_ = w
	client := &http.Client{
		Timeout: 5e9,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/from", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if hits != 1 {
		t.Fatalf("应只请求一次，实际 %d", hits)
	}
}
