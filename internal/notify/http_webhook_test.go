package notify

import (
	"net/http"
	"net/http/httptest"
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
	future := time.Now().UTC().Add(90 * time.Second)
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

	past := time.Now().UTC().Add(-time.Minute)
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
