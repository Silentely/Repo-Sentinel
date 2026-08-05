package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendTelegramIncludesReplyMarkup(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test message", "https://github.com/test/repo", "HTML")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	// 验证 reply_markup 存在
	rm, ok := receivedBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatal("期望 reply_markup 存在")
	}
	kb, ok := rm["inline_keyboard"].([]any)
	if !ok || len(kb) == 0 {
		t.Fatal("期望 inline_keyboard 非空")
	}
	// 验证按钮 URL
	row := kb[0].([]any)
	btn := row[0].(map[string]any)
	if btn["url"] != "https://github.com/test/repo" {
		t.Fatalf("期望按钮 URL 为 GitHub 链接，实际: %v", btn["url"])
	}
}

func TestSendTelegramWithoutURLNoReplyMarkup(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test message", "", "HTML")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	if _, ok := receivedBody["reply_markup"]; ok {
		t.Fatal("无 htmlURL 时不应包含 reply_markup")
	}
}

func TestSendTelegramParses429RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":120}}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test", "", "HTML")
	if err == nil {
		t.Fatal("期望 429 返回错误")
	}
	ra, ok := err.(*retryAfterError)
	if !ok {
		t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
	}
	if ra.seconds != 120 {
		t.Fatalf("期望 retry_after=120，实际: %d", ra.seconds)
	}
}

func TestSendTelegramParses429WithoutRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test", "", "HTML")
	if err == nil {
		t.Fatal("期望 429 返回错误")
	}
	ra, ok := err.(*retryAfterError)
	if !ok {
		t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
	}
	if ra.seconds != 30 {
		t.Fatalf("期望默认 retry_after=30，实际: %d", ra.seconds)
	}
}

func TestSendTelegramNotConfigured(t *testing.T) {
	w := &Worker{Client: http.DefaultClient}
	err := w.sendTelegramDirect(t.Context(), "http://unused", "", "token", "text", "", "HTML")
	if err == nil {
		t.Fatal("期望空 chatID 返回错误")
	}
}
