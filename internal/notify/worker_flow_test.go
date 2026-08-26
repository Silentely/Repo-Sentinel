package notify

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// newWorkerKeyRing 构造测试密钥环，供通道 SecretEnvelope 加密。
func newWorkerKeyRing(t *testing.T) *cryptox.KeyRing {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{CurrentKey: config.NewSecret(hex.EncodeToString(raw))})
	if err != nil {
		t.Fatal(err)
	}
	return &ring
}

// seedWebhookChannel 创建指向测试服务器的 HTTP Webhook 通道。
func seedWebhookChannel(t *testing.T, st store.Store, ring *cryptox.KeyRing, target string) store.NotificationChannel {
	t.Helper()
	env, err := ring.Encrypt(t.Context(), []byte("hook-secret"), []byte("reposentinel:notify-secret:v1"))
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID:             ulid.Make().String(),
		ChannelType:    store.ChannelHTTPWebhook,
		Name:           "test-hook",
		Enabled:        true,
		Target:         target,
		SecretEnvelope: env,
		AllowPrivate:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// TestWorkerTickDeliversOutbox 验证 tick 领取并投递 pending 条目，投递成功后转 sent。
func TestWorkerTickDeliversOutbox(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)

	var gotSignature string
	var gotBody []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-GitHub-Monitor-Signature-256")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch := seedWebhookChannel(t, st, ring, srv.URL)
	_, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|tick-deliver",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: "测试标题", BodyText: "测试内容", AttemptCount: 1,
		BodyJSON: map[string]any{"kind": "workflow_run"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sentFired := false
	w := &Worker{Store: st, KeyRing: ring, Client: srv.Client(), AAD: "reposentinel:notify-secret:v1", OnSent: func() { sentFired = true }}
	w.tick(t.Context())

	got, _, err := st.Outbox().List(t.Context(), store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 条 outbox，实际 %d", len(got))
	}
	if got[0].Status != store.OutboxSent {
		t.Fatalf("期望 sent，实际 %s", got[0].Status)
	}
	if !sentFired {
		t.Fatal("期望 OnSent 回调触发")
	}
	if gotSignature == "" || len(gotBody) == 0 {
		t.Fatal("期望请求携带签名与消息体")
	}
}

// TestWorkerTickDeliveryFailureRetries 验证投递失败时按阶梯重试而非死信。
func TestWorkerTickDeliveryFailureRetries(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ch := seedWebhookChannel(t, st, ring, srv.URL)
	if _, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|tick-fail",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: "t", BodyText: "b", AttemptCount: 1,
	}); err != nil {
		t.Fatal(err)
	}

	deadFired := false
	w := &Worker{Store: st, KeyRing: ring, Client: srv.Client(), AAD: "reposentinel:notify-secret:v1", OnDead: func() { deadFired = true }}
	w.tick(t.Context())

	got, _, err := st.Outbox().List(t.Context(), store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 条 outbox，实际 %d", len(got))
	}
	if got[0].Status != store.OutboxPending {
		t.Fatalf("普通失败应保持 pending 重试，实际 %s", got[0].Status)
	}
	// 语义化错误码：5xx 应直接展示 http_webhook_status_503，而不是笼统的 delivery_failed。
	if got[0].LastErrorCode != "http_webhook_status_503" {
		t.Fatalf("错误码 = %q, want http_webhook_status_503", got[0].LastErrorCode)
	}
	if deadFired {
		t.Fatal("未到上限不应触发死信")
	}
	if got[0].NextAttemptAt.Before(time.Now().UTC().Add(29 * time.Second)) {
		t.Fatalf("退避时间过早: %v", got[0].NextAttemptAt)
	}
}

// TestWorkerRunProcessesUntilCancel 验证 Run 循环在取消前持续投递。
func TestWorkerRunProcessesUntilCancel(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch := seedWebhookChannel(t, st, ring, srv.URL)
	if _, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|run-1",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: "t", BodyText: "b", AttemptCount: 1,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	w := &Worker{Store: st, KeyRing: ring, Client: srv.Client(), AAD: "reposentinel:notify-secret:v1"}
	go w.Run(ctx, 20*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := st.Outbox().CountByStatus(t.Context(), store.OutboxSent)
		if n >= 1 {
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	t.Fatal("Run 未在时限内投递条目")
}

// TestWorkerDecryptSecret 验证空信封/缺失密钥环/错误信封的处理。
func TestWorkerDecryptSecret(t *testing.T) {
	ring := newWorkerKeyRing(t)
	w := &Worker{KeyRing: ring, AAD: "reposentinel:notify-secret:v1"}

	// 空信封返回空串。
	if got, err := w.decryptSecret(t.Context(), ""); err != nil || got != "" {
		t.Fatalf("空信封应返回空串, got=%q err=%v", got, err)
	}
	// 缺失密钥环报错。
	w2 := &Worker{AAD: "reposentinel:notify-secret:v1"}
	if _, err := w2.decryptSecret(t.Context(), "some-envelope"); err == nil {
		t.Fatal("缺失密钥环应报错")
	}
	// 非法信封报错。
	if _, err := w.decryptSecret(t.Context(), "garbage"); err == nil {
		t.Fatal("非法信封应报错")
	}
	// 正确信封往返。
	env, err := ring.Encrypt(t.Context(), []byte("plain-secret"), []byte("reposentinel:notify-secret:v1"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.decryptSecret(t.Context(), env)
	if err != nil || got != "plain-secret" {
		t.Fatalf("解密不匹配: got=%q err=%v", got, err)
	}
}

// TestRetryAfterError 验证 retryAfterError 的错误字符串。
func TestRetryAfterError(t *testing.T) {
	e := &retryAfterError{seconds: 120, code: "telegram_rate_limited"}
	if got := e.Error(); got != "telegram_rate_limited" {
		t.Fatalf("Error() = %q, want telegram_rate_limited", got)
	}
}

// TestWorkerDeliverUnknownChannel 验证未知渠道类型报错。
func TestWorkerDeliverUnknownChannel(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)
	ch, err := st.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: "slack", Name: "unknown", Enabled: true, Target: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|unknown-ch",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(), Title: "t", BodyText: "b", AttemptCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{Store: st, KeyRing: ring, AAD: "reposentinel:notify-secret:v1"}
	// 空映射触发降级单查：渠道类型未知应报错（与 tick 内预加载映射命中同一语义）。
	if _, err := w.deliver(t.Context(), item, map[string]store.NotificationChannel{}, map[string]string{}); err == nil {
		t.Fatal("未知渠道应报错")
	}
}

// TestWorkerTickBodyJSONPreserved 验证 json 编码的 BodyJSON 不会破坏投递。
func TestWorkerTickBodyJSONPreserved(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)

	var gotEvent map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Event map[string]any `json:"event"`
		}
		_ = json.Unmarshal(body, &payload)
		gotEvent = payload.Event
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch := seedWebhookChannel(t, st, ring, srv.URL)
	if _, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|body-json",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: "t", BodyText: "b", AttemptCount: 1,
		BodyJSON: map[string]any{"kind": "issue", "action": "opened"},
	}); err != nil {
		t.Fatal(err)
	}

	w := &Worker{Store: st, KeyRing: ring, Client: srv.Client(), AAD: "reposentinel:notify-secret:v1"}
	w.tick(t.Context())
	if gotEvent == nil || gotEvent["kind"] != "issue" || gotEvent["action"] != "opened" {
		t.Fatalf("BodyJSON 未保留: %v", gotEvent)
	}
}

// TestTickDeliveredLogCarriesAttemptAndTruncatedTitle 投递成功日志应带 attempt 计数，
// 超长标题截断展示，避免单行日志膨胀。
func TestTickDeliveredLogCarriesAttemptAndTruncatedTitle(t *testing.T) {
	st := openWorkerTestStore(t)
	ring := newWorkerKeyRing(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch := seedWebhookChannel(t, st, ring, srv.URL)
	longTitle := strings.Repeat("长", 300)
	if _, err := st.Outbox().Create(t.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: "idem|log-title",
		Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
		Title: longTitle, BodyText: "b", AttemptCount: 2,
	}); err != nil {
		t.Fatal(err)
	}

	var logBuffer bytes.Buffer
	w := &Worker{
		Store: st, KeyRing: ring, Client: srv.Client(), AAD: "reposentinel:notify-secret:v1",
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, nil)),
	}
	w.tick(t.Context())

	logs := logBuffer.String()
	if !strings.Contains(logs, "notification delivered") {
		t.Fatalf("应记录投递成功日志，实际: %s", logs)
	}
	// ClaimDue 领取时递增尝试次数（2 → 3），日志应展示领取后的真实计数。
	if !strings.Contains(logs, `"attempt":3`) {
		t.Fatalf("投递成功日志应携带领取后的 attempt 计数，实际: %s", logs)
	}
	if strings.Contains(logs, longTitle) {
		t.Fatalf("超长标题应截断展示，实际: %s", logs)
	}
	if !strings.Contains(logs, "…") {
		t.Fatalf("截断标题应带省略号，实际: %s", logs)
	}
}

// TestTruncateLogTitle 标题截断边界：短标题原样返回，超长按码点截断不产生乱码。
func TestTruncateLogTitle(t *testing.T) {
	short := strings.Repeat("a", logTitleLimit)
	if got := truncateLogTitle(short); got != short {
		t.Fatalf("等于上限的标题不应截断，got len=%d", len(got))
	}
	long := strings.Repeat("中", logTitleLimit+10)
	got := truncateLogTitle(long)
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatal("截断结果不应包含替换字符乱码")
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("截断标题应带省略号，实际尾部: %q", got[len(got)-8:])
	}
}
