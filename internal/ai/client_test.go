package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// captureServer 记录请求并返回可控响应的桩服务器。
func captureServer(t *testing.T, status int, body string, capture func(*http.Request, []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture(r, raw)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestClientEnabled(t *testing.T) {
	if (&Client{}).IsEnabled() {
		t.Fatal("无 API Key 不应启用")
	}
	if !(&Client{APIKey: "k", Enabled: true}).IsEnabled() {
		t.Fatal("有 API Key 且总开关开启应启用")
	}
	if (&Client{APIKey: "k"}).IsEnabled() {
		t.Fatal("未开启总开关即使有 API Key 也不应启用")
	}
	if (&Client{Enabled: true}).IsEnabled() {
		t.Fatal("总开关开启但无 API Key 不应启用")
	}
	if (*Client)(nil).IsEnabled() {
		t.Fatal("nil 客户端不应启用")
	}
}

// TestDigestAndTriageEnabled 验证子功能开关与总开关/密钥的组合判定。
func TestDigestAndTriageEnabled(t *testing.T) {
	base := &Client{APIKey: "k", Enabled: true, DigestEnabled: true, TriageEnabled: true}
	if !base.IsDigestEnabled() || !base.IsTriageEnabled() {
		t.Fatal("全部开启时应可用")
	}
	noDigest := &Client{APIKey: "k", Enabled: true, DigestEnabled: false, TriageEnabled: true}
	if noDigest.IsDigestEnabled() {
		t.Fatal("digest_enabled=false 时摘要不可用")
	}
	if !noDigest.IsTriageEnabled() {
		t.Fatal("triage_enabled=true 时仍应可用")
	}
	// 总开关关闭时子功能一律不可用。
	off := &Client{APIKey: "k", Enabled: false, DigestEnabled: true}
	if off.IsDigestEnabled() || off.IsTriageEnabled() {
		t.Fatal("总开关关闭时子功能不可用")
	}
}

// TestHTTPClientReuse 验证未注入自定义 HTTP 时复用包级共享客户端（连接池复用）。
func TestHTTPClientReuse(t *testing.T) {
	c1 := &Client{}
	c2 := &Client{}
	if c1.httpClient() != c2.httpClient() {
		t.Fatal("未注入 HTTP 时应共享同一默认客户端")
	}
	injected := &http.Client{Timeout: time.Second}
	if (&Client{HTTP: injected}).httpClient() != injected {
		t.Fatal("注入的自定义客户端应原样使用")
	}
}

// TestTruncateOutput 验证 AI 输出长度上限：超长截断、边界不截断、多字节字符不截断半个字符。
func TestTruncateOutput(t *testing.T) {
	if got := truncateOutput("短文本"); got != "短文本" {
		t.Fatalf("短输出不应截断，实际: %q", got)
	}
	// 恰好等于上限不截断。
	exact := strings.Repeat("a", maxOutputRunes)
	if got := truncateOutput(exact); got != exact {
		t.Fatalf("等长输出不应截断，len=%d", len(got))
	}
	// 超长按 rune 截断，且不破坏多字节字符。
	long := strings.Repeat("界", maxOutputRunes+10)
	got := truncateOutput(long)
	if len([]rune(got)) != maxOutputRunes {
		t.Fatalf("超长输出应按 rune 截断到 %d，实际 %d", maxOutputRunes, len([]rune(got)))
	}
	if got != strings.Repeat("界", maxOutputRunes) {
		t.Fatal("截断不应破坏多字节字符边界")
	}
}

func TestCompleteSuccess(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"content":"  总结内容  "}}]}`, func(r *http.Request, raw []byte) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody = raw
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "test-key", Model: "test-model", Enabled: true}
	got, err := c.Complete(t.Context(), "system-prompt", "user-prompt")
	if err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	if got != "总结内容" {
		t.Fatalf("期望去掉首尾空白的内容，实际: %q", got)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("期望请求 /chat/completions，实际: %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("期望 Bearer 认证头，实际: %q", gotAuth)
	}
	var req chatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("请求体非 JSON: %v", err)
	}
	if req.Model != "test-model" {
		t.Fatalf("期望模型 test-model，实际: %s", req.Model)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[0].Content != "system-prompt" ||
		req.Messages[1].Role != "user" || req.Messages[1].Content != "user-prompt" {
		t.Fatalf("期望 system/user 双消息，实际: %+v", req.Messages)
	}
	if req.MaxTokens <= 0 {
		t.Fatalf("期望 max_tokens 为正，实际: %d", req.MaxTokens)
	}
}

func TestCompleteNotConfigured(t *testing.T) {
	c := &Client{}
	if _, err := c.Complete(t.Context(), "s", "u"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("无 Key 应返回 ErrNotConfigured，实际: %v", err)
	}
}

func TestCompleteHTTPError(t *testing.T) {
	srv := captureServer(t, http.StatusInternalServerError, `{"error":"boom"}`, nil)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
	if _, err := c.Complete(t.Context(), "s", "u"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("期望包含 HTTP 500 的错误，实际: %v", err)
	}
}

func TestCompleteBadResponse(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非法 JSON", `not-json`},
		{"缺 choices", `{}`},
		{"空内容", `{"choices":[{"message":{"content":"  "}}]}`},
	}
	for _, cse := range cases {
		t.Run(cse.name, func(t *testing.T) {
			srv := captureServer(t, http.StatusOK, cse.body, nil)
			defer srv.Close()
			c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
			if _, err := c.Complete(t.Context(), "s", "u"); err == nil {
				t.Fatalf("期望解析失败，实际成功")
			}
		})
	}
}

// TestCompleteNon2xxDetail 验证非 2xx 响应体被纳入错误信息（连通性测试可展示网关真实原因）。
func TestCompleteNon2xxDetail(t *testing.T) {
	srv := captureServer(t, http.StatusBadGateway,
		`upstream connect error or disconnect/reset before headers. reset reason: connection termination`, nil)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
	_, err := c.Complete(t.Context(), "s", "u")
	if err == nil {
		t.Fatal("502 应返回错误")
	}
	if !strings.Contains(err.Error(), "upstream connect error") {
		t.Fatalf("错误应包含响应体明细：%v", err)
	}
	// 无正文时返回稳定占位，不 panic。
	empty := captureServer(t, http.StatusInternalServerError, "", nil)
	defer empty.Close()
	if _, err := (&Client{BaseURL: empty.URL, APIKey: "k", Enabled: true}).Complete(t.Context(), "s", "u"); err == nil ||
		!strings.Contains(err.Error(), "empty response") {
		t.Fatalf("空正文应返回稳定占位：%v", err)
	}
}

func TestCompleteTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Timeout: 30 * time.Millisecond}
	if _, err := c.Complete(t.Context(), "s", "u"); err == nil {
		t.Fatal("期望超时错误")
	}
}

// TestCompleteResponseTooLarge 验证超限响应体被截断并解析失败（防御内存耗尽）。
func TestCompleteResponseTooLarge(t *testing.T) {
	huge := `{"choices":[{"message":{"content":"` + strings.Repeat("a", 2<<20) + `"}}]}`
	srv := captureServer(t, http.StatusOK, huge, nil)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
	if _, err := c.Complete(t.Context(), "s", "u"); err == nil {
		t.Fatal("超限响应体应解析失败")
	}
}

// TestCompleteContextCanceled 验证调用方取消 context 能中断请求。
func TestCompleteContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
	if _, err := c.Complete(ctx, "s", "u"); err == nil {
		t.Fatal("期望 context 取消错误")
	}
}

// TestClientPing 验证连通性探测：正常回复返回耗时，未配置与远端错误返回错误。
func TestClientPing(t *testing.T) {
	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`, nil)
	defer srv.Close()

	c := &Client{Enabled: true, APIKey: "k", BaseURL: srv.URL, Model: "probe-model", Timeout: time.Second, MaxTokens: 100}
	latency, err := c.Ping(t.Context())
	if err != nil {
		t.Fatalf("Ping 应成功：%v", err)
	}
	if latency <= 0 {
		t.Fatalf("耗时应为正数：%s", latency)
	}

	// 无 API Key / 未启用 / nil 客户端：ErrNotConfigured。
	if _, err := (&Client{}).Ping(t.Context()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("无 Key 应返回 ErrNotConfigured：%v", err)
	}
	if _, err := (&Client{APIKey: "k"}).Ping(t.Context()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("总开关关闭应返回 ErrNotConfigured：%v", err)
	}
	if _, err := (*Client)(nil).Ping(t.Context()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil 客户端应返回 ErrNotConfigured：%v", err)
	}

	// 远端 401 / 空响应：返回错误。
	bad := captureServer(t, http.StatusUnauthorized, "unauthorized", nil)
	defer bad.Close()
	if _, err := (&Client{Enabled: true, APIKey: "k", BaseURL: bad.URL}).Ping(t.Context()); err == nil {
		t.Fatal("HTTP 401 应返回错误")
	}
	empty := captureServer(t, http.StatusOK, `{"choices":[]}`, nil)
	defer empty.Close()
	if _, err := (&Client{Enabled: true, APIKey: "k", BaseURL: empty.URL}).Ping(t.Context()); err == nil {
		t.Fatal("空响应应返回错误")
	}
}

// newTestLogger 返回写入内存缓冲的 DEBUG 级 slog 记录器，供日志留痕断言。
func newTestLogger(t *testing.T) (*bytes.Buffer, *slog.Logger) {
	t.Helper()
	var buf bytes.Buffer
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelDebug)
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lv}))
	return &buf, logger
}

// TestCompleteLogsSuccess 验证成功调用留痕：DEBUG 发起 + INFO 成功与输出长度。
func TestCompleteLogsSuccess(t *testing.T) {
	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"content":"总结内容"}}]}`, nil)
	defer srv.Close()
	buf, logger := newTestLogger(t)
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Model: "m1", Logger: logger}
	got, err := c.Complete(t.Context(), "sys", "usr")
	if err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	if got != "总结内容" {
		t.Fatalf("返回值不应受日志影响，实际: %q", got)
	}
	out := buf.String()
	if !strings.Contains(out, `msg="ai request start"`) || !strings.Contains(out, "input_bytes=") {
		t.Fatalf("期望 DEBUG 发起日志，实际: %s", out)
	}
	if !strings.Contains(out, `msg="ai request ok"`) || !strings.Contains(out, "output_chars=") {
		t.Fatalf("期望 INFO 成功日志，实际: %s", out)
	}
	if !strings.Contains(out, "model=m1") {
		t.Fatalf("期望携带 model 字段，实际: %s", out)
	}
}

// TestCompleteLogsUpstreamError 验证上游非 2xx 分类为 upstream_<status> 且带响应明细。
func TestCompleteLogsUpstreamError(t *testing.T) {
	srv := captureServer(t, http.StatusBadGateway, `upstream connect error`, nil)
	defer srv.Close()
	buf, logger := newTestLogger(t)
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Logger: logger}
	if _, err := c.Complete(t.Context(), "s", "u"); err == nil {
		t.Fatal("502 应返回错误")
	}
	out := buf.String()
	if !strings.Contains(out, `msg="ai request failed"`) {
		t.Fatalf("期望 WARN 失败日志，实际: %s", out)
	}
	if !strings.Contains(out, "error_code=upstream_502") {
		t.Fatalf("期望错误分类 upstream_502，实际: %s", out)
	}
	if !strings.Contains(out, "upstream connect error") {
		t.Fatalf("期望日志含上游返回内容，实际: %s", out)
	}
}

// TestCompleteLogsTimeout 验证超时分类为 timeout。
func TestCompleteLogsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	buf, logger := newTestLogger(t)
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Timeout: 30 * time.Millisecond, Logger: logger}
	if _, err := c.Complete(t.Context(), "s", "u"); err == nil {
		t.Fatal("期望超时错误")
	}
	out := buf.String()
	if !strings.Contains(out, `msg="ai request failed"`) || !strings.Contains(out, "error_code=timeout") {
		t.Fatalf("期望错误分类 timeout，实际: %s", out)
	}
}

// TestCompleteLogsNotConfiguredSilent 验证未配置时不发起请求也不留痕（上层按降级处理）。
func TestCompleteLogsNotConfiguredSilent(t *testing.T) {
	buf, logger := newTestLogger(t)
	c := &Client{Logger: logger}
	if _, err := c.Complete(t.Context(), "s", "u"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("无 Key 应返回 ErrNotConfigured，实际: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("未配置不应产生日志，实际: %s", buf.String())
	}
}

// TestClassifyCallError 验证失败分类映射：网络/超时/上游/坏响应/空响应/未知。
func TestClassifyCallError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"网络错误", &callError{code: "network", err: errors.New("ai: request failed: dial tcp")}, "network"},
		{"上游 429", &callError{code: "upstream", status: 429, err: errors.New("ai: http 429: rate limited")}, "upstream_429"},
		{"坏响应", &callError{code: "bad_response", err: errors.New("ai: decode response: boom")}, "bad_response"},
		{"空响应", &callError{code: "empty_response", err: errors.New("ai: empty response")}, "empty_response"},
		{"裸超时", context.DeadlineExceeded, "timeout"},
		{"包装超时", &callError{code: "timeout", err: fmt.Errorf("ai: request timeout after 30ms: %w", context.DeadlineExceeded)}, "timeout"},
		{"并发上限", &callError{code: "concurrency_limit", err: errors.New("ai: wait for concurrency slot: context deadline exceeded")}, "concurrency_limit"},
		{"未知", errors.New("whatever"), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code, _ := classifyCallError(tc.err); code != tc.want {
				t.Fatalf("期望分类 %s，实际 %s", tc.want, code)
			}
		})
	}
}

// TestCompleteLogsUsage 验证成功日志携带上游返回的 token 用量（成本可观测）。
func TestCompleteLogsUsage(t *testing.T) {
	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"content":"总结"}}],"usage":{"prompt_tokens":42,"completion_tokens":7,"total_tokens":49}}`, nil)
	defer srv.Close()
	buf, logger := newTestLogger(t)
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Logger: logger}
	if _, err := c.Complete(t.Context(), "s", "u"); err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "prompt_tokens=42") || !strings.Contains(out, "completion_tokens=7") {
		t.Fatalf("期望成功日志携带 token 用量，实际: %s", out)
	}
}

// TestCompleteMetrics 验证指标与日志同源：成功累计请求/耗时/token，失败按 error_code 分列。
func TestCompleteMetrics(t *testing.T) {
	// 成功路径。
	req0, fail0, dur0, prompt0, comp0, _ := MetricsSnapshot()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 至少 1ms 耗时，保证耗时累计断言可测。
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}
	if _, err := c.Complete(t.Context(), "s", "u"); err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	req1, fail1, dur1, prompt1, comp1, _ := MetricsSnapshot()
	if req1 != req0+1 {
		t.Fatalf("请求计数应 +1，期望 %d 实际 %d", req0+1, req1)
	}
	if fail1 != fail0 {
		t.Fatalf("失败计数不应增加，期望 %d 实际 %d", fail0, fail1)
	}
	if dur1 <= dur0 {
		t.Fatal("耗时累计应增加")
	}
	if prompt1 != prompt0+10 || comp1 != comp0+5 {
		t.Fatalf("token 计数应累计，期望 prompt %d/completion %d，实际 %d/%d", prompt0+10, comp0+5, prompt1, comp1)
	}

	// 失败路径：按 error_code 分列。
	req2, fail2, _, _, _, byCode2 := MetricsSnapshot()
	bad := captureServer(t, http.StatusBadGateway, `boom`, nil)
	defer bad.Close()
	cb := &Client{BaseURL: bad.URL, APIKey: "k", Enabled: true}
	if _, err := cb.Complete(t.Context(), "s", "u"); err == nil {
		t.Fatal("502 应失败")
	}
	req3, fail3, _, _, _, byCode3 := MetricsSnapshot()
	if req3 != req2+1 || fail3 != fail2+1 {
		t.Fatalf("失败应同时累计请求与失败计数，期望 req %d/fail %d，实际 %d/%d", req2+1, fail2+1, req3, fail3)
	}
	if byCode3["upstream_502"] != byCode2["upstream_502"]+1 {
		t.Fatalf("失败次数应按 error_code 分列，期望 %d 实际 %d", byCode2["upstream_502"]+1, byCode3["upstream_502"])
	}
}

// TestCompleteReqID 验证请求关联 ID：显式注入沿用，未注入自动生成且 start/ok 一致。
func TestCompleteReqID(t *testing.T) {
	srv := captureServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"}}]}`, nil)
	defer srv.Close()

	// 显式注入：调用日志复用同一 req_id。
	buf, logger := newTestLogger(t)
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Logger: logger}
	if _, err := c.Complete(WithRequestID(t.Context(), "test-req-1"), "s", "u"); err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	if !strings.Contains(buf.String(), "req_id=test-req-1") {
		t.Fatalf("期望日志携带注入的 req_id，实际: %s", buf.String())
	}

	// 未注入：自动生成非空 ID，且同一调用各条日志一致。
	buf2, logger2 := newTestLogger(t)
	c2 := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Logger: logger2}
	if _, err := c2.Complete(t.Context(), "s", "u"); err != nil {
		t.Fatalf("Complete 应成功: %v", err)
	}
	re := regexp.MustCompile(`req_id=([0-9a-f]+)`)
	ids := re.FindAllStringSubmatch(buf2.String(), -1)
	if len(ids) < 2 {
		t.Fatalf("期望 start/ok 日志均携带 req_id，实际: %s", buf2.String())
	}
	if ids[0][1] != ids[1][1] {
		t.Fatalf("同一调用各日志 req_id 应一致，实际: %s", buf2.String())
	}
}

// TestEnsureRequestID 验证关联 ID 生成/沿用语义。
func TestEnsureRequestID(t *testing.T) {
	ctx, id := EnsureRequestID(t.Context())
	if id == "" {
		t.Fatal("未注入时应生成非空 ID")
	}
	if RequestIDFromContext(ctx) != id {
		t.Fatal("注入后应可读回同一 ID")
	}
	ctx2, id2 := EnsureRequestID(WithRequestID(t.Context(), "keep-me"))
	if id2 != "keep-me" || RequestIDFromContext(ctx2) != "keep-me" {
		t.Fatalf("已注入时应沿用，实际 %q", id2)
	}
	if RequestIDFromContext(nil) != "" {
		t.Fatal("nil ctx 应返回空串")
	}
}

// TestConcurrencyBudget 验证并发预算：槽位被占用时后续调用排队而非立即失败，释放后继续。
func TestConcurrencyBudget(t *testing.T) {
	old := aiMaxConcurrency
	aiMaxConcurrency = 1
	t.Cleanup(func() { aiMaxConcurrency = old })

	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Timeout: 5 * time.Second}

	// A 先占用唯一槽位（HTTP 处理被卡住，槽位未释放）。
	aDone := make(chan error, 1)
	go func() { _, err := c.Complete(t.Context(), "s", "u"); aDone <- err }()
	<-entered

	// B 应排队等待，而非立即失败。
	bDone := make(chan error, 1)
	go func() { _, err := c.Complete(t.Context(), "s", "u"); bDone <- err }()
	select {
	case err := <-bDone:
		t.Fatalf("B 应等待槽位而非立即返回: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// 释放 A 后 A/B 依次完成。
	close(release)
	select {
	case err := <-aDone:
		if err != nil {
			t.Fatalf("A 失败: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("A 超时")
	}
	select {
	case err := <-bDone:
		if err != nil {
			t.Fatalf("B 失败: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("B 应获得槽位后完成")
	}
}

// TestCompleteConcurrencyLimitContextCanceled 验证等待槽位超出 ctx 预算时以 concurrency_limit 失败。
func TestCompleteConcurrencyLimitContextCanceled(t *testing.T) {
	old := aiMaxConcurrency
	aiMaxConcurrency = 1
	t.Cleanup(func() { aiMaxConcurrency = old })

	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true, Timeout: 5 * time.Second}

	aDone := make(chan error, 1)
	go func() { _, err := c.Complete(t.Context(), "s", "u"); aDone <- err }()
	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Complete(ctx, "s", "u"); err == nil {
		t.Fatal("等待槽位超预算应返回错误")
	} else if code, _ := classifyCallError(err); code != "concurrency_limit" {
		t.Fatalf("期望分类 concurrency_limit，实际 %s（%v）", code, err)
	}
	close(release)
	<-aDone
}
