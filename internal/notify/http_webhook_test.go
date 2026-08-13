package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// newWebhookHarness 构造指向测试服务器的通道与条目，直接调用 sendHTTP 验证投递行为。
func newWebhookHarness(t *testing.T, handler http.HandlerFunc) (*Worker, store.NotificationChannel, store.NotificationOutbox) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	ch := store.NotificationChannel{
		ChannelType:  store.ChannelHTTPWebhook,
		Target:       srv.URL,
		AllowPrivate: true, // 测试服务器监听 127.0.0.1，需放行私网校验
	}
	item := store.NotificationOutbox{
		ID: "01JTESTWEBHOOK000000000001", Title: "t", BodyText: "b",
		BodyJSON: map[string]any{"kind": "workflow_run"},
	}
	return &Worker{Client: srv.Client()}, ch, item
}

// Retry-After 为整数秒或缺失/非法时：合法值转为 retryAfterError，其余回退固定阶梯。
func TestSendHTTPHonorsRetryAfterHeader(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		retryAfter  string
		wantSeconds int
		wantCode    string // 空表示期望普通错误（非 retryAfterError）
	}{
		{"429整数秒", http.StatusTooManyRequests, "120", 120, "http_webhook_retry_after"},
		{"503整数秒", http.StatusServiceUnavailable, "5", 5, "http_webhook_retry_after"},
		{"429无响应头", http.StatusTooManyRequests, "", 0, ""},
		{"429非法响应头", http.StatusTooManyRequests, "abc", 0, ""},
		{"429零秒不采纳", http.StatusTooManyRequests, "0", 0, ""},
		{"429超出上限封顶", http.StatusTooManyRequests, "999999", maxRetryAfter, "http_webhook_retry_after"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					rw.Header().Set("Retry-After", tc.retryAfter)
				}
				rw.WriteHeader(tc.status)
			})
			err := w.sendHTTP(t.Context(), ch, "", item)
			if err == nil {
				t.Fatal("期望非 2xx 返回错误")
			}
			ra, ok := err.(*retryAfterError)
			if tc.wantCode == "" {
				if ok {
					t.Fatalf("不期望 retryAfterError，实际 seconds=%d code=%s", ra.seconds, ra.code)
				}
				return
			}
			if !ok {
				t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
			}
			if ra.seconds != tc.wantSeconds {
				t.Fatalf("期望 seconds=%d，实际 %d", tc.wantSeconds, ra.seconds)
			}
			if ra.code != tc.wantCode {
				t.Fatalf("期望 code=%s，实际 %s", tc.wantCode, ra.code)
			}
		})
	}
}

// Retry-After 为 HTTP 日期时按剩余秒数退避；日期已过期则回退固定阶梯。
func TestSendHTTPHonorsRetryAfterHTTPDate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(90 * time.Second)
	w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Retry-After", future.Format(http.TimeFormat))
		rw.WriteHeader(http.StatusTooManyRequests)
	})
	err := w.sendHTTP(t.Context(), ch, "", item)
	ra, ok := err.(*retryAfterError)
	if !ok {
		t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
	}
	if ra.seconds != 90 {
		t.Fatalf("期望 seconds=90，实际 %d", ra.seconds)
	}

	past := now.Add(-time.Minute)
	w2, ch2, item2 := newWebhookHarness(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Retry-After", past.Format(http.TimeFormat))
		rw.WriteHeader(http.StatusTooManyRequests)
	})
	if err := w2.sendHTTP(t.Context(), ch2, "", item2); err == nil {
		t.Fatal("期望非 2xx 返回错误")
	} else if _, ok := err.(*retryAfterError); ok {
		t.Fatalf("已过期的日期不应生成 retryAfterError: %v", err)
	}
}

// 超大的错误响应体只保留有限前缀，并明确提示已截断，避免上游错误页膨胀日志与 Outbox 错误详情。
func TestSendHTTPTruncatesOversizedErrorBody(t *testing.T) {
	const prefix = `{"error":"upstream exploded"}`
	oversized := prefix + strings.Repeat("x", bodyDetailLimit*4)
	w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte(oversized))
	})

	err := w.sendHTTP(t.Context(), ch, "", item)
	if err == nil {
		t.Fatal("502 应返回错误")
	}
	if !strings.Contains(err.Error(), prefix) {
		t.Fatalf("错误应保留响应体前缀，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "响应体已截断") {
		t.Fatalf("错误应说明响应体已截断，实际: %v", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", bodyDetailLimit+1)) {
		t.Fatal("错误不应携带超过响应体上限的连续内容")
	}
}

// 出站 payload 必须同时携带 HTML 正文（body_text，兼容既有接收端）与纯文本（body_plain，
// 供无 HTML 解析能力的接收端直接消费），且 body_plain 不含残留标签。
func TestSendHTTPIncludesPlainBody(t *testing.T) {
	var receivedBody map[string]any
	w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		receivedBody = body
		rw.WriteHeader(http.StatusOK)
	})
	item.BodyText = `<b>🟢 已打开｜修复登录 Bug</b>
────────────────
📦 仓库：<code>org/repo</code>
<a href="https://github.com/org/repo/issues/42">🔗 在 GitHub 中查看</a>`
	if err := w.sendHTTP(t.Context(), ch, "", item); err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if receivedBody["body_text"] != item.BodyText {
		t.Fatal("body_text 应保留原 HTML 正文")
	}
	plain, ok := receivedBody["body_plain"].(string)
	if !ok || plain == "" {
		t.Fatal("payload 应包含 body_plain 纯文本")
	}
	if strings.ContainsAny(plain, "<>") {
		t.Fatalf("body_plain 不应含 HTML 标签: %q", plain)
	}
	if !strings.Contains(plain, "org/repo") {
		t.Fatalf("body_plain 应包含仓库名: %q", plain)
	}
	if !strings.Contains(plain, "https://github.com/org/repo/issues/42") {
		t.Fatalf("body_plain 应保留链接 URL: %q", plain)
	}
}

// TestSendHTTPSetsUserAgent 出站投递必须携带明确 User-Agent：
// 接收端日志/过滤据此识别 RepoSentinel，而非 Go 默认客户端 UA。
func TestSendHTTPSetsUserAgent(t *testing.T) {
	var gotUA string
	w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		rw.WriteHeader(http.StatusOK)
	})
	if err := w.sendHTTP(t.Context(), ch, "", item); err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if gotUA != "RepoSentinel-Webhook/1.0" {
		t.Fatalf("User-Agent=%q，期望 RepoSentinel-Webhook/1.0", gotUA)
	}
}

// htmlToPlainText 标签剔除与实体反转义的正确性。
func TestHTMLToPlainText(t *testing.T) {
	got := htmlToPlainText(`<b>标题</b> &amp; <code>x &lt; y</code> <a href="https://example.com/a?b=1&amp;c=2">链接</a>`)
	// 标签被剔除；&lt; 反转义后的 < 属于正文内容而非标签。
	if strings.Contains(got, "<b>") || strings.Contains(got, "<code>") || strings.Contains(got, "</") {
		t.Fatalf("结果不应残留标签: %q", got)
	}
	if !strings.Contains(got, "标题") || !strings.Contains(got, "&") {
		t.Fatalf("实体应反转义: %q", got)
	}
	if !strings.Contains(got, "x < y") {
		t.Fatalf("&lt; 应反转义为 <: %q", got)
	}
	if !strings.Contains(got, "链接 (https://example.com/a?b=1&c=2)") {
		t.Fatalf("链接应保留文字与 URL: %q", got)
	}
}

// 出站客户端禁跟随重定向（ErrUseLastResponse）：3xx 说明目标返回重定向而接收端未收到
// 通知，必须按可重试错误处理；此前会落到 success 分支被标记 sent，造成静默丢失。
func TestSendHTTPRedirectIsFailure(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w, ch, item := newWebhookHarness(t, func(rw http.ResponseWriter, _ *http.Request) {
				rw.WriteHeader(status)
			})
			err := w.sendHTTP(t.Context(), ch, "", item)
			if err == nil {
				t.Fatalf("%d 应返回错误（接收端未收到通知）", status)
			}
			code := deliveryErrorCode(err)
			if want := fmt.Sprintf("http_webhook_redirect_%d", status); code != want {
				t.Fatalf("期望错误码 %s，实际 %s", want, code)
			}
		})
	}
}
