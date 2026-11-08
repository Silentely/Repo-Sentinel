package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTelegramAPIBase = "https://api.telegram.org"

// SetBotMenuButton 调用 Telegram Bot API setChatMenuButton 端点，将机器人默认菜单按钮配置为打开 TMA 控制台。
func SetBotMenuButton(ctx context.Context, botToken, menuURL string) error {
	client := newSafeHTTPClient(10 * time.Second)
	return SetBotMenuButtonWithClient(ctx, client, defaultTelegramAPIBase, botToken, menuURL)
}

// SetBotMenuButtonWithClient 支持自定义 client 与 apiBase，便于单元测试与反代环境。
func SetBotMenuButtonWithClient(ctx context.Context, client *http.Client, apiBase, botToken, menuURL string) error {
	trimmedToken := strings.TrimSpace(botToken)
	if trimmedToken == "" {
		return fmt.Errorf("bot_token is required")
	}
	trimmedMenuURL := strings.TrimSpace(menuURL)
	if trimmedMenuURL == "" {
		return fmt.Errorf("menu_url is required")
	}

	if client == nil {
		client = newSafeHTTPClient(10 * time.Second)
	}
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = defaultTelegramAPIBase
	}

	endpoint := fmt.Sprintf("%s/bot%s/setChatMenuButton", base, trimmedToken)

	requestBody := map[string]any{
		"menu_button": map[string]any{
			"type": "web_app",
			"text": "控制台",
			"web_app": map[string]string{
				"url": trimmedMenuURL,
			},
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal setChatMenuButton payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create setChatMenuButton request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("setChatMenuButton request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("failed to read setChatMenuButton response: %w", err)
	}

	var tgResp struct {
		OK          bool   `json:"ok"`
		Result      bool   `json:"result"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBytes, &tgResp); err != nil {
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("setChatMenuButton http status %d: %s", resp.StatusCode, string(respBytes))
		}
		return fmt.Errorf("failed to decode setChatMenuButton response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || !tgResp.OK {
		return fmt.Errorf("telegram setChatMenuButton error %d: %s", tgResp.ErrorCode, tgResp.Description)
	}

	return nil
}
