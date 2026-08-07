package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTruncateTelegramTextKeepsShortText(t *testing.T) {
	short := strings.Repeat("a", telegramTextLimit-1)
	if got := truncateTelegramText(short); got != short {
		t.Fatalf("短文本不应截断，got len=%d", len(got))
	}
	// 恰好等于上限同样不截断。
	exact := strings.Repeat("a", telegramTextLimit)
	if got := truncateTelegramText(exact); got != exact {
		t.Fatal("等于上限的文本不应截断")
	}
}

func TestTruncateTelegramTextCutsLongText(t *testing.T) {
	long := strings.Repeat("a", telegramTextLimit+500)
	got := truncateTelegramText(long)
	if len([]rune(got)) > telegramTextLimit+20 {
		t.Fatalf("截断后长度应接近上限，got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "已截断）") {
		t.Fatalf("截断后应带省略提示，实际尾部: %q", got[len(got)-12:])
	}
}

func TestTruncateTelegramTextAvoidsBrokenTag(t *testing.T) {
	// 截断点落在 <a href="..." 开标签中间：必须回退到标签之前，不能把残缺标签发给 Telegram。
	text := strings.Repeat("a", telegramTextLimit-10) + `<a href="https://github.com/org/repo/issues/123"`
	got := truncateTelegramText(text)
	if strings.Contains(got, "<a href") {
		t.Fatalf("截断结果不应包含未闭合标签: %q", got[len(got)-60:])
	}
}

func TestTruncateTelegramTextAvoidsBrokenEntity(t *testing.T) {
	// 截断点落在实体中间（&amp 无分号）：必须剔除残缺实体。
	text := strings.Repeat("a", telegramTextLimit-3) + "&amp"
	got := truncateTelegramText(text)
	if strings.HasSuffix(got, "&amp") {
		t.Fatalf("截断结果不应以未闭合实体结尾: %q", got[len(got)-12:])
	}
}

func TestTruncateTelegramTextKeepsMultibyteIntact(t *testing.T) {
	// 多字节 UTF-8（中文）按码点截断，不得产生替换字符乱码。
	long := strings.Repeat("中", telegramTextLimit+100)
	got := truncateTelegramText(long)
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatal("截断结果不应包含替换字符乱码")
	}
}
