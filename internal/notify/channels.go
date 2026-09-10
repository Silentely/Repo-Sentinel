package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// truncateRunes 安全截断字符串到指定 rune 数量，防止截断多字节字符。
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if len(s) <= maxRunes || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for i := range s {
		if count == maxRunes {
			return s[:i] + "…"
		}
		count++
	}
	return s
}

// sendFeishu 发送飞书/Lark 自定义机器人消息。
func (w *Worker) sendFeishu(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	now := time.Now().UTC()
	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 1500)

	elements := []any{
		map[string]any{
			"tag": "markdown",
			"content": fmt.Sprintf("**%s**\n\n%s", item.Title, plainBody),
		},
	}
	if item.HTMLURL != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{
				map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": store.GitHubViewLabel},
					"url":  item.HTMLURL,
					"type": "primary",
				},
			},
		})
	}

	payload := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": truncateRunes(item.Title, 100)},
				"template": "blue",
			},
			"elements": elements,
		},
	}

	// 飞书签名校验
	if secret != "" {
		ts := strconv.FormatInt(now.Unix(), 10)
		stringToSign := fmt.Sprintf("%s\n%s", ts, secret)
		mac := hmac.New(sha256.New, []byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		payload["timestamp"] = ts
		payload["sign"] = sign
	}

	return w.postJSONChannel(ctx, ch, target, "feishu", payload)
}

// sendWeCom 发送企业微信群机器人消息。
func (w *Worker) sendWeCom(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 2000)
	var sb strings.Builder
	sb.WriteString("### " + item.Title + "\n\n")
	sb.WriteString(plainBody)
	if item.HTMLURL != "" {
		sb.WriteString(fmt.Sprintf("\n\n[%s](%s)", store.GitHubViewLabel, item.HTMLURL))
	}

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"content": sb.String(),
		},
	}

	return w.postJSONChannel(ctx, ch, target, "wecom", payload)
}

// sendDingTalk 发送钉钉自定义机器人消息。
func (w *Worker) sendDingTalk(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	// 如果有加签密钥，追加 timestamp 与 sign query 参数
	reqURL := target
	if secret != "" {
		nowMs := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
		stringToSign := fmt.Sprintf("%s\n%s", nowMs, secret)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = fmt.Sprintf("%s%stimestamp=%s&sign=%s", reqURL, sep, nowMs, sign)
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 2000)
	var payload map[string]any

	if item.HTMLURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          item.Title,
				"text":           fmt.Sprintf("### %s\n\n%s", item.Title, plainBody),
				"singleTitle":    store.GitHubViewLabel,
				"singleURL":      item.HTMLURL,
				"btnOrientation": "0",
			},
		}
	} else {
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"title": item.Title,
				"text":  fmt.Sprintf("### %s\n\n%s", item.Title, plainBody),
			},
		}
	}

	return w.postJSONChannel(ctx, ch, reqURL, "dingtalk", payload)
}

// sendDiscord 发送 Discord Webhook 消息。
func (w *Worker) sendDiscord(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 2000)
	embed := map[string]any{
		"title":       item.Title,
		"description": plainBody,
		"color":       3447003, // 蓝色 (0x3498DB)
	}
	if item.HTMLURL != "" {
		embed["url"] = item.HTMLURL
	}

	payload := map[string]any{
		"embeds": []any{embed},
	}

	return w.postJSONChannel(ctx, ch, target, "discord", payload)
}

// sendBark 发送 Bark 推送消息。
func (w *Worker) sendBark(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	// 如果 target 未带 http/https 前缀，尝试补充默认 Bark 官方地址
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "https://api.day.app/" + target
	}
	// 如果配置了 secret 且 target 结尾没有该 key
	if secret != "" && !strings.HasSuffix(target, secret) {
		target = strings.TrimRight(target, "/") + "/" + secret
	}
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 1000)
	payload := map[string]any{
		"title": item.Title,
		"body":  plainBody,
		"group": "RepoSentinel",
	}
	if item.HTMLURL != "" {
		payload["url"] = item.HTMLURL
	}

	return w.postJSONChannel(ctx, ch, target, "bark", payload)
}

// postJSONChannel 通用 JSON 投递辅助方法。
func (w *Worker) postJSONChannel(ctx context.Context, ch store.NotificationChannel, targetURL, channelTag string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(withAllowPrivate(ctx, ch.AllowPrivate), http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent)

	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return deliveryErrorf(fmt.Sprintf("%s_redirect_%d", channelTag, resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
		if ra := parseRetryAfter(resp); ra > 0 {
			return &retryAfterError{seconds: ra, code: channelTag + "_retry_after"}
		}
		return deliveryErrorf(fmt.Sprintf("%s_status_%d", channelTag, resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode >= 400 {
		return deliveryErrorf(fmt.Sprintf("%s_client_error_%d", channelTag, resp.StatusCode), readBodyDetail(resp))
	}

	// 针对部分返回 200 但在 Body 中报告错误的平台（如飞书 errcode!=0 / 企业微信 errcode!=0 / 钉钉 errcode!=0）
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, bodyDetailLimit))
	if len(bodyBytes) > 0 {
		if channelTag == "bark" {
			var barkResp struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(bodyBytes, &barkResp) == nil && barkResp.Code != 0 && barkResp.Code != 200 {
				return deliveryErrorf(fmt.Sprintf("%s_client_error_%d", channelTag, barkResp.Code), barkResp.Message)
			}
		} else {
			var statusResp struct {
				Code    int    `json:"code"`
				ErrCode int    `json:"errcode"`
				Msg     string `json:"msg"`
				ErrMsg  string `json:"errmsg"`
			}
			if json.Unmarshal(bodyBytes, &statusResp) == nil {
				if statusResp.Code != 0 {
					return deliveryErrorf(fmt.Sprintf("%s_client_error_%d", channelTag, statusResp.Code), statusResp.Msg)
				}
				if statusResp.ErrCode != 0 {
					return deliveryErrorf(fmt.Sprintf("%s_client_error_%d", channelTag, statusResp.ErrCode), statusResp.ErrMsg)
				}
			}
		}
	}

	return nil
}
