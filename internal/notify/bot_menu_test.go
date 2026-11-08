package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
