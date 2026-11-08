package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSetBotMenuButton_Success(t *testing.T) {
	var receivedBody map[string]any
	var receivedPath string
	var receivedMethod string
	var contentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(srv.Close)

	ctx := t.Context()
	botToken := "test-bot-token-123"
	menuURL := "https://sentinel.example.com/tma/dashboard"

	err := SetBotMenuButtonWithClient(ctx, srv.Client(), srv.URL, botToken, menuURL)
	if err != nil {
		t.Fatalf("期望成功但返回错误: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("期望 POST 请求，实际: %s", receivedMethod)
	}
	expectedPath := "/bot" + botToken + "/setChatMenuButton"
	if receivedPath != expectedPath {
		t.Errorf("请求路径不符: want %s, got %s", expectedPath, receivedPath)
	}
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("期望 Content-Type application/json, 实际: %s", contentType)
	}

	mb, ok := receivedBody["menu_button"].(map[string]any)
	if !ok {
		t.Fatalf("期望包含 menu_button 字段: %v", receivedBody)
	}
	if mb["type"] != "web_app" {
		t.Errorf("menu_button.type 不符: %v", mb["type"])
	}
	if mb["text"] != "控制台" {
		t.Errorf("menu_button.text 不符: %v", mb["text"])
	}
	webApp, ok := mb["web_app"].(map[string]any)
	if !ok {
		t.Fatalf("menu_button.web_app 不符: %v", mb["web_app"])
	}
	if webApp["url"] != menuURL {
		t.Errorf("menu_button.web_app.url 不符: want %s, got %v", menuURL, webApp["url"])
	}
}

func TestSetBotMenuButton_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	t.Cleanup(srv.Close)

	ctx := t.Context()
	err := SetBotMenuButtonWithClient(ctx, srv.Client(), srv.URL, "fake-token", "https://sentinel.example.com/tma/dashboard")
	if err == nil {
		t.Fatal("期望 API 错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("错误信息应包含状态码和描述: %v", err)
	}
}

func TestSetBotMenuButton_InvalidArgs(t *testing.T) {
	ctx := t.Context()
	if err := SetBotMenuButton(ctx, "", "https://sentinel.example.com/tma/dashboard"); err == nil {
		t.Fatal("空 botToken 应报错")
	}
	if err := SetBotMenuButton(ctx, "fake-token", ""); err == nil {
		t.Fatal("空 menuURL 应报错")
	}
}

func TestSetBotMenuButton_NeverLeaksTokenInErrorsAndLogs(t *testing.T) {
	botToken := "987654321:AAFE_TestSecretTokenShouldNeverBeLeaked_xyz"
	menuURL := "https://sentinel.example.com/tma/dashboard"

	// 1. Direct SetBotMenuButtonWithClient call failure (unreachable endpoint)
	client := &http.Client{
		Transport: &roundTripperFunc{
			fn: func(req *http.Request) (*http.Response, error) {
				return nil, &url.Error{
					Op:  req.Method,
					URL: req.URL.String(),
					Err: errors.New("dial tcp: lookup telegram.invalid: no such host"),
				}
			},
		},
	}

	err := SetBotMenuButtonWithClient(context.Background(), client, "https://telegram.invalid", botToken, menuURL)
	if err == nil {
		t.Fatal("期望请求失败但返回 nil")
	}

	errStr := err.Error()
	if strings.Contains(errStr, botToken) {
		t.Fatalf("错误信息中泄露了完整 botToken: %s", errStr)
	}
	if strings.Contains(errStr, "987654321:") {
		t.Fatalf("错误信息中泄露了 token 前缀: %s", errStr)
	}
	if telegramBotTokenRegex.MatchString(errStr) {
		t.Fatalf("错误信息匹配到疑似 Telegram Bot Token 模式: %s", errStr)
	}

	// 2. Direct LogSafeDeliveryError check
	rawURLErr := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot" + botToken + "/setChatMenuButton",
		Err: errors.New("connection reset by peer"),
	}
	sanitized := LogSafeDeliveryError(rawURLErr)
	if strings.Contains(sanitized, botToken) {
		t.Fatalf("LogSafeDeliveryError 泄露了完整 botToken: %s", sanitized)
	}
	if strings.Contains(sanitized, "987654321") {
		t.Fatalf("LogSafeDeliveryError 泄露了 token 数字段: %s", sanitized)
	}
	if telegramBotTokenRegex.MatchString(sanitized) {
		t.Fatalf("LogSafeDeliveryError 结果中仍匹配到 bot token 正则: %s", sanitized)
	}

	// 3. Logger buffer check
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	logger.Warn("tma: setChatMenuButton failed",
		"error_code", "telegram_set_chat_menu_button_failed",
		"action", "setChatMenuButton",
		"error", LogSafeDeliveryError(err),
	)
	logOutput := logBuf.String()
	if strings.Contains(logOutput, botToken) {
		t.Fatalf("日志输出中泄露了 botToken: %s", logOutput)
	}
	if strings.Contains(logOutput, "987654321") {
		t.Fatalf("日志输出中泄露了 token 前缀: %s", logOutput)
	}
	if telegramBotTokenRegex.MatchString(logOutput) {
		t.Fatalf("日志输出中匹配到了 bot token 正则: %s", logOutput)
	}
}

type roundTripperFunc struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (r *roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.fn(req)
}
