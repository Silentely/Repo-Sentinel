package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

const webhookTestSecret = "webhook-单测密钥-0123456789"

// webhookTestOptions 控制 webhook 测试 fixture 的可变依赖。
type webhookTestOptions struct {
	runtimeSecret string            // 经 GitHubRuntime 注入的当前 webhook secret
	configSecret  string            // 经 Config.GitHub.WebhookSecret 注入（此时 GitHubRuntime 置 nil）
	omitRuntime   bool              // 强制 GitHubRuntime 为 nil（验证 Config 分支且不配置 secret）
	background    context.Context   // 异步规范化上下文；nil 时 processWebhookAsync 直接返回
	aggregator    *rules.Aggregator // 注入的规则聚合器；nil 时走 rules.Engine 回退
	logger        *slog.Logger      // 默认丢弃输出
}

// webhookTestFixture 与 newHTTPTestFixture 等价，但额外支持注入 Background、Aggregator
// 以及 Config 级 secret：既有 fixture 不暴露 Dependencies，webhook 异步处理又依赖
// Background 上下文，无法满足异步断言，故在本文件内单独实现。
// srv 与 handler 由同一组 Dependencies 构造（New 内部会自建 server 实例）；
// srv 仅作为 safeGo 的宿主用于后台 panic 用例，其 safeGo 只依赖共享的 Logger，
// 与 handler 内部 server 的 safeGo 行为等价。
type webhookTestFixture struct {
	srv     *server
	handler http.Handler
	store   store.Store
}

func newWebhookTestFixture(t *testing.T, opts webhookTestOptions) *webhookTestFixture {
	t.Helper()
	// Atlas 直接使用 sql.DB 时会在 TMPDIR 下取固定 SQLite 锁名，每个测试必须隔离。
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(temporaryDir, "webhook.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开 webhook 测试 Store 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := opened.Close(); err != nil {
			t.Errorf("关闭 webhook 测试 Store 失败: %v", err)
		}
	})

	cfg := config.Config{
		HTTP:     config.HTTPConfig{PublicBaseURL: "https://reposentinel.example"},
		Database: config.DatabaseConfig{Driver: "sqlite"},
	}
	var ghRuntime *githubx.RuntimeConfig
	switch {
	case opts.runtimeSecret != "":
		ghRuntime = &githubx.RuntimeConfig{
			WebhookSecret:       opts.runtimeSecret,
			WebhookSecretSource: "env",
		}
	case opts.configSecret != "" || opts.omitRuntime:
		// GitHubRuntime 为 nil 时 handler 回退到 Config.GitHub.WebhookSecret 分支。
		ghRuntime = nil
		if opts.configSecret != "" {
			cfg.GitHub.WebhookSecret = config.NewSecret(opts.configSecret)
		}
	default:
		// 默认有 Runtime 但不配置任何 secret，用于 webhook_not_configured 用例。
		ghRuntime = &githubx.RuntimeConfig{WebhookSecretSource: "unset"}
	}
	logger := opts.logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	dependencies := Dependencies{
		Config:        cfg,
		Store:         opened,
		Logger:        logger,
		GitHubRuntime: ghRuntime,
		Background:    opts.background,
		Aggregator:    opts.aggregator,
	}
	return &webhookTestFixture{
		srv:     &server{dependencies: dependencies},
		handler: New(dependencies),
		store:   opened,
	}
}

// postWebhook 发送一次 GitHub webhook POST 请求。
func (f *webhookTestFixture) postWebhook(t *testing.T, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:47001"
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// signWebhookBody 按 X-Hub-Signature-256 规则计算 HMAC-SHA256 签名（与 githubx.VerifySignature 对应）。
func signWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// signedHeaders 生成一次合法投递所需的全部请求头。
func signedHeaders(secret, deliveryID, eventType string, body []byte) map[string]string {
	return map[string]string{
		"X-Hub-Signature-256": signWebhookBody(secret, body),
		"X-GitHub-Delivery":   deliveryID,
		"X-GitHub-Event":      eventType,
	}
}

// issueOpenedPayload 构造最小可用的 issues/opened 载荷（仓库固定 acme/app）。
func issueOpenedPayload(t *testing.T, number int, updatedAt time.Time) []byte {
	t.Helper()
	payload := map[string]any{
		"action": "opened",
		"repository": map[string]any{
			"id":        int64(2200000001), // 超过 int32 上限，顺带覆盖 bigint 列
			"name":      "app",
			"full_name": "acme/app",
			"owner":     map[string]any{"login": "acme"},
		},
		"issue": map[string]any{
			"number":     number,
			"title":      "webhook 单测 issue",
			"state":      "open",
			"html_url":   "https://github.com/acme/app/issues/" + strconv.Itoa(number),
			"user":       map[string]any{"login": "octocat"},
			"updated_at": updatedAt.UTC().Format(time.RFC3339),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}
	return body
}

// waitForDeliverySettled 轮询 delivery 直到脱离 accepted 或超时，避免固定 sleep 造成 flaky。
func waitForDeliverySettled(t *testing.T, st store.Store, deliveryID string, timeout time.Duration) store.WebhookDelivery {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		got, err := st.WebhookDeliveries().GetByDeliveryID(ctx, deliveryID)
		if err == nil && got.Status != store.DeliveryAccepted {
			return got
		}
		if time.Now().After(deadline) {
			lastStatus := "记录不存在"
			if err == nil {
				lastStatus = got.Status
			}
			t.Fatalf("等待 delivery %s 终态超时（最后状态=%s）", deliveryID, lastStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertNoDelivery 断言指定 deliveryID 未写入 webhook_deliveries 表。
func assertNoDelivery(t *testing.T, st store.Store, deliveryID string) {
	t.Helper()
	_, err := st.WebhookDeliveries().GetByDeliveryID(context.Background(), deliveryID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delivery %s 不应入库，err=%v", deliveryID, err)
	}
}

func TestWebhook验签缺失或错误返回401且不入库(t *testing.T) {
	fixture := newWebhookTestFixture(t, webhookTestOptions{runtimeSecret: webhookTestSecret})
	body := []byte(`{"action":"opened"}`)

	// 完全缺失 X-Hub-Signature-256 头。
	missing := fixture.postWebhook(t, body, map[string]string{
		"X-GitHub-Delivery": "delivery-no-sig",
		"X-GitHub-Event":    "issues",
	})
	assertAPIError(t, missing, http.StatusUnauthorized, "invalid_signature")

	// 签名错误（用错误密钥计算）。
	wrong := fixture.postWebhook(t, body, map[string]string{
		"X-Hub-Signature-256": signWebhookBody("错误的密钥", body),
		"X-GitHub-Delivery":   "delivery-bad-sig",
		"X-GitHub-Event":      "issues",
	})
	assertAPIError(t, wrong, http.StatusUnauthorized, "invalid_signature")

	// 验签失败不得写入 webhook_deliveries。
	assertNoDelivery(t, fixture.store, "delivery-no-sig")
	assertNoDelivery(t, fixture.store, "delivery-bad-sig")
}

func TestWebhook未配置Secret返回503(t *testing.T) {
	// GitHubRuntime 存在但没有任何 secret。
	noRuntimeSecret := newWebhookTestFixture(t, webhookTestOptions{})
	resp := noRuntimeSecret.postWebhook(t, []byte(`{}`), map[string]string{
		"X-Hub-Signature-256": "sha256=whatever",
		"X-GitHub-Delivery":   "delivery-unconfigured",
		"X-GitHub-Event":      "issues",
	})
	assertAPIError(t, resp, http.StatusServiceUnavailable, "webhook_not_configured")
	assertNoDelivery(t, noRuntimeSecret.store, "delivery-unconfigured")

	// GitHubRuntime 为 nil 且 Config 也未配置 secret。
	noConfigSecret := newWebhookTestFixture(t, webhookTestOptions{omitRuntime: true})
	respFallback := noConfigSecret.postWebhook(t, []byte(`{}`), map[string]string{
		"X-Hub-Signature-256": "sha256=whatever",
		"X-GitHub-Delivery":   "delivery-unconfigured-fallback",
		"X-GitHub-Event":      "issues",
	})
	assertAPIError(t, respFallback, http.StatusServiceUnavailable, "webhook_not_configured")
	assertNoDelivery(t, noConfigSecret.store, "delivery-unconfigured-fallback")
}

func TestWebhook合法签名接受投递并入库accepted(t *testing.T) {
	fixture := newWebhookTestFixture(t, webhookTestOptions{runtimeSecret: webhookTestSecret})
	body := issueOpenedPayload(t, 1, time.Now().UTC())

	resp := fixture.postWebhook(t, body, signedHeaders(webhookTestSecret, "delivery-accept-1", "issues", body))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("状态码=%d，期望 202；响应=%s", resp.Code, resp.Body.String())
	}
	var ack struct {
		Status     string `json:"status"`
		DeliveryID string `json:"delivery_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &ack); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if ack.Status != "accepted" || ack.DeliveryID != "delivery-accept-1" {
		t.Fatalf("响应=(%q, %q)，期望 accepted/delivery-accept-1", ack.Status, ack.DeliveryID)
	}

	// Background 为 nil 时异步处理直接返回，行稳定停留在 accepted，可同步断言。
	got, err := fixture.store.WebhookDeliveries().GetByDeliveryID(context.Background(), "delivery-accept-1")
	if err != nil {
		t.Fatalf("查询 delivery 失败: %v", err)
	}
	if got.Status != store.DeliveryAccepted || got.EventType != "issues" {
		t.Fatalf("入库状态=(%q, %q)，期望 accepted/issues", got.Status, got.EventType)
	}
	if !bytes.Equal(got.Payload, body) {
		t.Fatal("入库 payload 与请求体不一致")
	}

	// Config secret 分支（GitHubRuntime 为 nil）同样验签通过。
	fixtureCfg := newWebhookTestFixture(t, webhookTestOptions{configSecret: webhookTestSecret})
	bodyCfg := issueOpenedPayload(t, 2, time.Now().UTC())
	respCfg := fixtureCfg.postWebhook(t, bodyCfg, signedHeaders(webhookTestSecret, "delivery-accept-2", "issues", bodyCfg))
	if respCfg.Code != http.StatusAccepted {
		t.Fatalf("Config 分支状态码=%d，期望 202；响应=%s", respCfg.Code, respCfg.Body.String())
	}
	gotCfg, err := fixtureCfg.store.WebhookDeliveries().GetByDeliveryID(context.Background(), "delivery-accept-2")
	if err != nil || gotCfg.Status != store.DeliveryAccepted {
		t.Fatalf("Config 分支入库=(%+v, %v)，期望 accepted", gotCfg, err)
	}
}

func TestWebhook重复投递去重仅保留一行(t *testing.T) {
	fixture := newWebhookTestFixture(t, webhookTestOptions{runtimeSecret: webhookTestSecret})
	body := issueOpenedPayload(t, 1, time.Now().UTC())
	headers := signedHeaders(webhookTestSecret, "delivery-dup", "issues", body)

	first := fixture.postWebhook(t, body, headers)
	if first.Code != http.StatusAccepted {
		t.Fatalf("首次投递状态码=%d，期望 202；响应=%s", first.Code, first.Body.String())
	}
	second := fixture.postWebhook(t, body, headers)
	if second.Code != http.StatusAccepted {
		t.Fatalf("重复投递状态码=%d，期望 202；响应=%s", second.Code, second.Body.String())
	}
	var ack struct {
		Status     string `json:"status"`
		DeliveryID string `json:"delivery_id"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &ack); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if ack.Status != "duplicate" || ack.DeliveryID != "delivery-dup" {
		t.Fatalf("重复投递响应=(%q, %q)，期望 duplicate/delivery-dup", ack.Status, ack.DeliveryID)
	}

	// GetByDeliveryID 走 ent Only：恰好一行才会成功返回，多行会报错。
	got, err := fixture.store.WebhookDeliveries().GetByDeliveryID(context.Background(), "delivery-dup")
	if err != nil {
		t.Fatalf("查询重复 delivery 失败: %v", err)
	}
	if got.Status != store.DeliveryAccepted {
		t.Fatalf("重复投递不应改动原有状态，got %q", got.Status)
	}
	// delivery_id 唯一索引仍生效：再插入同 deliveryID 必然冲突，二次证明表内仅一行。
	if _, err := fixture.store.WebhookDeliveries().Create(context.Background(), store.WebhookDelivery{
		DeliveryID: "delivery-dup", EventType: "issues", Payload: []byte(`{}`),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("同 deliveryID 再插入应返回冲突，err=%v", err)
	}
}

func TestWebhook缺少必需头返回400(t *testing.T) {
	fixture := newWebhookTestFixture(t, webhookTestOptions{runtimeSecret: webhookTestSecret})
	// 头校验在验签之后，必须携带合法签名才能触达 400 分支。
	body := issueOpenedPayload(t, 1, time.Now().UTC())
	signature := signWebhookBody(webhookTestSecret, body)

	noDelivery := fixture.postWebhook(t, body, map[string]string{
		"X-Hub-Signature-256": signature,
		"X-GitHub-Event":      "issues",
	})
	assertAPIError(t, noDelivery, http.StatusBadRequest, "validation_failed")

	noEvent := fixture.postWebhook(t, body, map[string]string{
		"X-Hub-Signature-256": signature,
		"X-GitHub-Delivery":   "delivery-missing-event",
	})
	assertAPIError(t, noEvent, http.StatusBadRequest, "validation_failed")

	assertNoDelivery(t, fixture.store, "delivery-missing-event")
}

func TestWebhook超大请求体返回413(t *testing.T) {
	fixture := newWebhookTestFixture(t, webhookTestOptions{runtimeSecret: webhookTestSecret})
	// maxWebhookBody = 1 MiB；超限 body 在读取阶段即失败，先于验签，签名合法与否不影响结果。
	oversized := bytes.Repeat([]byte("a"), (1<<20)+1)
	resp := fixture.postWebhook(t, oversized, map[string]string{
		"X-Hub-Signature-256": signWebhookBody(webhookTestSecret, oversized),
		"X-GitHub-Delivery":   "delivery-oversized",
		"X-GitHub-Event":      "issues",
	})
	assertAPIError(t, resp, http.StatusRequestEntityTooLarge, "validation_failed")
	assertNoDelivery(t, fixture.store, "delivery-oversized")
}

// 异步 panic 用例说明：通读 internal/normalizer/process.go 后确认 Process 对全部畸形输入
// （非法 JSON、缺失字段、未知 event type）都返回 error，没有可稳定触达的 panic 路径；
// 因此按保底方案直接驱动 safeGo，验证 recover 会记录日志且进程保持可用。
func TestWebhook后台Panic被Recover且不影响进程(t *testing.T) {
	logBuffer := &lockedBuffer{}
	fixture := newWebhookTestFixture(t, webhookTestOptions{
		runtimeSecret: webhookTestSecret,
		logger:        slog.New(slog.NewJSONHandler(logBuffer, nil)),
	})

	// 与被测代码相同的任务名，模拟 processWebhookAsync 在后台 goroutine 中 panic。
	fixture.srv.safeGo("webhook_process", func() {
		panic("webhook 异步处理模拟崩溃")
	})

	// 轮询等待 recover 日志落盘（2 秒超时，避免固定 sleep）。
	deadline := time.Now().Add(2 * time.Second)
	for {
		logs := logBuffer.String()
		if strings.Contains(logs, "background task panic") && strings.Contains(logs, "webhook_process") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("后台 panic 未被 safeGo 记录；日志=%s", logs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// panic 未拖垮进程：同一服务的 webhook 链路仍可正常处理请求（未签名 → 401）。
	resp := fixture.postWebhook(t, []byte(`{}`), map[string]string{
		"X-GitHub-Delivery": "delivery-after-panic",
		"X-GitHub-Event":    "issues",
	})
	assertAPIError(t, resp, http.StatusUnauthorized, "invalid_signature")
}

// lockedBuffer 是带锁的日志缓冲，供后台 goroutine 写、主 goroutine 轮询读。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestWebhook关闭期状态标记不受Background取消影响 回归验证 processWebhookAsync 内
// markCtx（context.WithoutCancel(Background)+5s 超时，见 webhook_handlers.go）的修复：
// 优雅关闭瞬间 Background 已被 App 取消，delivery 行不得永久停留在 accepted。
// 本用例让 Background 在处理开始前就已取消：规范化首个 DB 调用必然以
// context.Canceled 失败走 normalize_failed 分支，但 MarkProcessed 必须脱离取消
// 仍然提交成功，行终态为 failed 而非 accepted。
func TestWebhook关闭期状态标记不受Background取消影响(t *testing.T) {
	// 处理开始前即取消，模拟优雅关闭瞬间。
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	fixture := newWebhookTestFixture(t, webhookTestOptions{
		runtimeSecret: webhookTestSecret,
		background:    cancelled,
	})

	// 手工写入 accepted 行：等价于 202 已返回、异步处理尚未执行的瞬间。
	body := issueOpenedPayload(t, 201, time.Now().UTC())
	row, err := fixture.store.WebhookDeliveries().Create(context.Background(), store.WebhookDelivery{
		ID:         "wd-shutdown-mark",
		DeliveryID: "delivery-shutdown-mark",
		EventType:  "issues",
		Status:     store.DeliveryAccepted,
		Payload:    body,
		ReceivedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("预置 accepted delivery 失败: %v", err)
	}

	// 直接同步调用（不经 safeGo），调用返回即处理完毕，无需轮询。
	fixture.srv.processWebhookAsync(row.ID, "issues", "delivery-shutdown-mark", body)

	got, err := fixture.store.WebhookDeliveries().GetByDeliveryID(context.Background(), "delivery-shutdown-mark")
	if err != nil {
		t.Fatalf("回读 delivery 失败: %v", err)
	}
	// 核心回归断言：状态必须脱离 accepted。若 markCtx 未用 WithoutCancel 脱离取消，
	// MarkProcessed 会以已取消 ctx 静默失败（返回值被忽略），行将永久停留 accepted。
	if got.Status == store.DeliveryAccepted {
		t.Fatalf("关闭期标记失败：状态仍为 accepted；行=%+v", got)
	}
	// Background 已取消 → 规范化必然 context.Canceled 失败 → normalize_failed 分支。
	if got.Status != store.DeliveryFailed || got.ErrorCode != "normalize_failed" {
		t.Fatalf("终态=(%q, %q)，期望 failed/normalize_failed", got.Status, got.ErrorCode)
	}
	// ProcessedAt 由 MarkProcessed 一并写入，非空进一步证明标记确实提交成功。
	if got.ProcessedAt == nil {
		t.Fatal("ProcessedAt 为空：MarkProcessed 未成功提交")
	}
}

func TestWebhook规则评估失败标记rule_failed(t *testing.T) {
	// 注入一个必然失败的 Aggregator：底层 Store 已关闭，超频（burst）降级路径调用
	// Channels().List 时必然返回错误，从而使 Aggregator.Evaluate 返回 error。
	// BurstThreshold=1：同仓第二条实时事件（BurstWindow 内）即触发 burst 路径。
	closedStore, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(t.TempDir(), "closed.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开待关闭 Store 失败: %v", err)
	}
	if err := closedStore.Close(); err != nil {
		t.Fatalf("关闭 Store 失败: %v", err)
	}
	aggregator := rules.NewAggregator(closedStore, time.Minute, 1, time.Minute)

	fixture := newWebhookTestFixture(t, webhookTestOptions{
		runtimeSecret: webhookTestSecret,
		background:    t.Context(),
		aggregator:    aggregator,
	})
	ctx := context.Background()
	// 预置 active 仓库：否则 webhook 新建的 baseline 仓库会抑制实时通知，走不到规则评估。
	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-wh-rule", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "app", FullName: "acme/app",
	}); err != nil {
		t.Fatalf("预置仓库失败: %v", err)
	}

	// 首条 issue 事件：正常入桶（flush 定时器窗口内不触发），delivery 终态应为 processed。
	bodyOK := issueOpenedPayload(t, 101, time.Now().UTC())
	respOK := fixture.postWebhook(t, bodyOK, signedHeaders(webhookTestSecret, "delivery-rule-ok", "issues", bodyOK))
	if respOK.Code != http.StatusAccepted {
		t.Fatalf("首条投递状态码=%d，期望 202；响应=%s", respOK.Code, respOK.Body.String())
	}
	if got := waitForDeliverySettled(t, fixture.store, "delivery-rule-ok", 2*time.Second); got.Status != store.DeliveryProcessed {
		t.Fatalf("首条 delivery 终态=%q，期望 processed", got.Status)
	}

	// 第二条 issue 事件（同仓、BurstWindow 内）：触发 burst → Evaluate 返回错误 → rule_failed。
	bodyFail := issueOpenedPayload(t, 102, time.Now().UTC())
	respFail := fixture.postWebhook(t, bodyFail, signedHeaders(webhookTestSecret, "delivery-rule-fail", "issues", bodyFail))
	if respFail.Code != http.StatusAccepted {
		t.Fatalf("第二条投递状态码=%d，期望 202；响应=%s", respFail.Code, respFail.Body.String())
	}
	gotFail := waitForDeliverySettled(t, fixture.store, "delivery-rule-fail", 2*time.Second)
	if gotFail.Status != store.DeliveryFailed || gotFail.ErrorCode != "rule_failed" {
		t.Fatalf("第二条 delivery 终态=(%q, %q)，期望 failed/rule_failed", gotFail.Status, gotFail.ErrorCode)
	}
}
