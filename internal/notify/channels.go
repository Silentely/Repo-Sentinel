package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
			return strings.TrimRight(s[:i], " \n\r\t") + "…"
		}
		count++
	}
	return s
}

// themeSeverityKeywords 为判定主题色所用的**词级**关键词表。
// 必须用词边界匹配，禁止裸子串匹配：此前 "high" 会命中 "highlight(s)"、"success" 会命中
// "successful"/"succession"，把普通通知渲染成高危红色，削弱告警可信度。
var (
	themeCriticalKeywords = []string{
		"failure", "failed", "fail", "critical", "high", "block risk", "block-risk",
		"high risk", "high-risk", "high severity", "high-severity",
	}
	themeWarningKeywords = []string{
		"warning", "warn", "medium", "needs manual review", "needs tests", "needs test",
	}
	themeSuccessKeywords = []string{
		"success", "succeeded", "passed", "ready to merge", "fixed", "resolved",
	}
)

// containsAnyWord 判定文本（已小写）是否包含任一**完整单词**关键词。
// 词边界按非字母数字字符划分，故 "block risk" 这类含空格的短语仍可整体匹配。
func containsAnyWord(lowerText string, keywords []string) bool {
	for _, kw := range keywords {
		if containsWord(lowerText, kw) {
			return true
		}
	}
	return false
}

// containsWord 判定小写文本是否含完整单词/短语 kw（两侧不得紧邻字母数字字符）。
func containsWord(lowerText, kw string) bool {
	idx := 0
	for {
		pos := strings.Index(lowerText[idx:], kw)
		if pos < 0 {
			return false
		}
		start := idx + pos
		end := start + len(kw)
		if !isWordCharBefore(lowerText, start) && !isWordCharAt(lowerText, end) {
			return true
		}
		idx = start + 1
		if idx >= len(lowerText) {
			return false
		}
	}
}

func isWordCharBefore(s string, i int) bool {
	return i > 0 && isWordByte(s[i-1])
}

func isWordCharAt(s string, i int) bool {
	return i < len(s) && isWordByte(s[i])
}

// isWordByte 判定字节是否属于「单词字符」（ASCII 字母/数字/连字符）。
// 关键词表均为 ASCII，非 ASCII 字节（如中文）一律视为分隔符，使
// 「失败failure」这类中英混排也能正确切出边界。
// 连字符计入单词字符，使 "high-level"、"build-failed" 视为一个整体词，
// 避免 "high" 命中 "high-level" 这类良性复合词。
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-'
}

// determineMessageTheme 根据标题与正文语义智能判定消息主题色：
// 飞书卡片 header template 支持: carmine (红/高危/失败), orange (橙/警示/需关注), turquoise (青绿/成功/健康), blue (默认蓝)。
// Discord embed color 支持: 0xEF4444, 0xF59E0B, 0x10B981, 0x3B82F6。
// 标题上的状态 emoji 为强信号直接命中；正文走词边界关键词匹配，避免子串误判。
func determineMessageTheme(title, body string) (feishuColor string, discordColor int) {
	if containsAnyRune(title, "❌", "🚨", "🔴") {
		return "carmine", 0xEF4444
	}
	if containsAnyRune(title, "⚠️", "⚠", "🟠", "🟡") {
		return "orange", 0xF59E0B
	}
	if containsAnyRune(title, "✅", "🟢", "🟣") {
		return "turquoise", 0x10B981
	}
	lower := strings.ToLower(title + " " + body)
	if containsAnyWord(lower, themeCriticalKeywords) {
		return "carmine", 0xEF4444
	}
	if containsAnyWord(lower, themeWarningKeywords) {
		return "orange", 0xF59E0B
	}
	if containsAnyWord(lower, themeSuccessKeywords) {
		return "turquoise", 0x10B981
	}
	return "blue", 0x3B82F6
}

// containsAnyRune 判定文本是否包含任一字面量标记（emoji/符号）。
func containsAnyRune(text string, marks ...string) bool {
	for _, m := range marks {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// aiSectionDelimiter 为规则引擎追加 AI 分析段落时使用的分隔标记（分隔线 + 🤖 + 段标题）。
// 仅匹配 AI 段落：digest/摘要模式使用的不带 🤖 的分隔线不受影响。
const aiSectionDelimiter = "\n────────────────\n🤖 "

// splitAISections 将通知正文拆为「主体前缀 + AI 段落列表」，供各渠道按自身语法渲染。
// 无 AI 段落时原样返回输入（保留原始换行），段落为 [标题, 正文]（正文已去除首尾空白）。
// 收敛于此：此前企业微信/钉钉用两处 ReplaceAll、Telegram 用自带的 Split，三处各自维护分隔符字面量。
func splitAISections(text string) (prefix string, sections [][2]string) {
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), aiSectionDelimiter)
	if len(parts) == 1 {
		return text, nil
	}
	prefix = parts[0]
	for _, p := range parts[1:] {
		lines := strings.SplitN(strings.TrimSpace(p), "\n", 2)
		title := strings.TrimSpace(lines[0])
		var body string
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		sections = append(sections, [2]string{title, body})
	}
	return prefix, sections
}

// formatQuotedAISections 将 AI 段落渲染为引用块（企业微信/钉钉 markdown 语法）：
// 段标题与正文逐行加 "> " 前缀，使整段落在引用内，而非只把分隔线换成 "> 🤖" 让正文裸露在引用之外。
func formatQuotedAISections(text string) string {
	prefix, sections := splitAISections(text)
	if len(sections) == 0 {
		return text
	}
	var sb strings.Builder
	sb.WriteString(prefix)
	for _, sec := range sections {
		sb.WriteString("\n> 🤖 " + sec[0])
		if sec[1] == "" {
			continue
		}
		for _, line := range strings.Split(sec[1], "\n") {
			sb.WriteString("\n> " + line)
		}
	}
	return sb.String()
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
			"tag":     "markdown",
			"content": "**" + item.Title + "**\n\n" + plainBody,
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

	feishuColor, _ := determineMessageTheme(item.Title, item.BodyText)
	payload := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": truncateRunes(item.Title, 100)},
				"template": feishuColor,
			},
			"elements": elements,
		},
	}

	// 飞书签名校验
	if secret != "" {
		ts := strconv.FormatInt(now.Unix(), 10)
		stringToSign := ts + "\n" + secret
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
	plainBody = formatQuotedAISections(plainBody)
	var sb strings.Builder
	sb.WriteString("### " + item.Title + "\n\n")
	sb.WriteString(plainBody)
	if item.HTMLURL != "" {
		sb.WriteString("\n\n[" + store.GitHubViewLabel + "](" + item.HTMLURL + ")")
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
		stringToSign := nowMs + "\n" + secret
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = reqURL + sep + "timestamp=" + nowMs + "&sign=" + sign
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 2000)
	plainBody = formatQuotedAISections(plainBody)
	var payload map[string]any

	if item.HTMLURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          item.Title,
				"text":           "### " + item.Title + "\n\n" + plainBody,
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
				"text":  "### " + item.Title + "\n\n" + plainBody,
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
	_, discordColor := determineMessageTheme(item.Title, item.BodyText)
	embed := map[string]any{
		"title":       item.Title,
		"description": plainBody,
		"color":       discordColor,
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
		return deliveryErrorf(channelTag+"_redirect_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
		if ra := parseRetryAfter(resp); ra > 0 {
			return &retryAfterError{seconds: ra, code: channelTag + "_retry_after"}
		}
		return deliveryErrorf(channelTag+"_status_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode >= 400 {
		return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
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
				msg := barkResp.Message
				if msg == "" {
					msg = strings.TrimSpace(string(bodyBytes))
				}
				return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(barkResp.Code), msg)
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
					msg := statusResp.Msg
					if msg == "" {
						msg = statusResp.ErrMsg
					}
					if msg == "" {
						msg = strings.TrimSpace(string(bodyBytes))
					}
					return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(statusResp.Code), msg)
				}
				if statusResp.ErrCode != 0 {
					msg := statusResp.ErrMsg
					if msg == "" {
						msg = statusResp.Msg
					}
					if msg == "" {
						msg = strings.TrimSpace(string(bodyBytes))
					}
					return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(statusResp.ErrCode), msg)
				}
			}
		}
	}

	return nil
}
