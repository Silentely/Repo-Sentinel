package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultTelegramAPIBaseURL = "https://api.telegram.org"

type setChatMenuButtonRequest struct {
	MenuButton menuButton `json:"menu_button"`
}

type menuButton struct {
	Type   string     `json:"type"`
	Text   string     `json:"text"`
	WebApp webAppInfo `json:"web_app"`
}

type webAppInfo struct {
	URL string `json:"url"`
}

type telegramResponse struct {
	Ok          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// SetBotMenuButton 配置 Telegram Bot 的聊天菜单按钮为打开 TMA 控制台
func SetBotMenuButton(ctx context.Context, botToken, menuURL string) error {
	return SetBotMenuButtonWithClient(ctx, http.DefaultClient, defaultTelegramAPIBaseURL, botToken, menuURL)
}

// SetBotMenuButtonWithClient 使用指定的 HTTP 客户端和 API 基础地址配置菜单按钮（方便单元测试注入 mock server）
func SetBotMenuButtonWithClient(ctx context.Context, client *http.Client, apiBaseURL, botToken, menuURL string) error {
	if botToken == "" {
		return fmt.Errorf("botToken must not be empty")
	}
	if menuURL == "" {
		return fmt.Errorf("menuURL must not be empty")
	}
	if client == nil {
		client = http.DefaultClient
	}
	if apiBaseURL == "" {
		apiBaseURL = defaultTelegramAPIBaseURL
	}
	endpoint := fmt.Sprintf("%s/bot%s/setChatMenuButton", strings.TrimRight(apiBaseURL, "/"), botToken)

	reqBody := setChatMenuButtonRequest{
		MenuButton: menuButton{
			Type: "web_app",
			Text: "控制台",
			WebApp: webAppInfo{
				URL: menuURL,
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal setChatMenuButton request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create setChatMenuButton request: %s", logSafeDeliveryError(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("setChatMenuButton request failed: %s", logSafeDeliveryError(err))
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read setChatMenuButton response: %w", err)
	}

	var tgResp telegramResponse
	if err := json.Unmarshal(respBytes, &tgResp); err != nil {
		return fmt.Errorf("failed to parse setChatMenuButton response: %w", err)
	}

	if !tgResp.Ok {
		return fmt.Errorf("setChatMenuButton returned error: code=%d description=%s", tgResp.ErrorCode, tgResp.Description)
	}

	return nil
}
