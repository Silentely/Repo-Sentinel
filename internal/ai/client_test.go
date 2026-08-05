package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
