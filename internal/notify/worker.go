package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// defaultBackoff 固定退避阶梯（数组不可变：长度固定，避免误 append 打乱节奏）。
var defaultBackoff = [...]time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	4 * time.Hour,
	8 * time.Hour,
	12 * time.Hour,
}

const maxAttempts = 8

// AAD 通知密钥加密的附加认证数据。必须与写入端（app/bootstrap、httpapi 渠道管理）保持一致，
// 否则历史加密的渠道密钥将无法解密，故收敛为单一来源。
const AAD = "reposentinel:notify-secret:v1"

// Worker 投递 Outbox。
type Worker struct {
	Store   store.Store
	KeyRing *cryptox.KeyRing
	Client  *http.Client
	Logger  *slog.Logger
	AAD     string
	// OnSent / OnDead 可选指标回调，避免 notify 包依赖 httpapi。
	OnSent func()
	OnDead func()
}

// Run 循环领取并发送。
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if w.Client == nil {
		w.Client = newSafeHTTPClient(15 * time.Second)
	}
	if w.AAD == "" {
		w.AAD = AAD
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	items, err := w.Store.Outbox().ClaimDue(ctx, time.Now().UTC(), 2*time.Minute, 20)
	if err != nil {
		if w.Logger != nil {
			// 领取失败必须携带真实错误，否则数据库抖动时只有 error_code 无法区分
			// 是连接、锁还是迁移问题。
			w.Logger.Error("outbox claim failed", "error_code", "database_unavailable", "error", err.Error())
		}
		return
	}
	for _, item := range items {
		channelType, err := w.deliver(ctx, item)
		if err != nil {
			if w.Logger != nil {
				w.Logger.Warn(
					"notification delivery failed",
					"outbox_id", item.ID,
					"channel_id", item.ChannelID,
					"channel_type", channelType,
					"attempt", item.AttemptCount,
					"error", err.Error(),
				)
			}
			w.handleFailure(ctx, item, err)
			continue
		}
		if err := w.Store.Outbox().MarkSent(ctx, item.ID); err != nil {
			// 标记失败会让条目下次 ClaimDue 被重新投递：记录日志便于排查重复通知来源。
			if w.Logger != nil {
				w.Logger.Error(
					"notification delivered but mark failed",
					"outbox_id", item.ID,
					"channel_id", item.ChannelID,
					"channel_type", channelType,
					"error_code", "outbox_mark_failed",
					"error", err.Error(),
				)
			}
			// 标记未落库不算投递成功，不触发 sent 指标。
			continue
		}
		if w.Logger != nil {
			w.Logger.Info(
				"notification delivered",
				"outbox_id", item.ID,
				"channel_id", item.ChannelID,
				"channel_type", channelType,
				"attempt", item.AttemptCount,
				"title", truncateLogTitle(item.Title),
			)
		}
		if w.OnSent != nil {
			w.OnSent()
		}
	}
}

func (w *Worker) deliver(ctx context.Context, item store.NotificationOutbox) (string, error) {
	ch, err := w.Store.Channels().Get(ctx, item.ChannelID)
	if err != nil {
		return "", err
	}
	secret, err := w.decryptSecret(ctx, ch.SecretEnvelope)
	if err != nil {
		return ch.ChannelType, fmt.Errorf("decrypt_secret: %w", err)
	}
	switch ch.ChannelType {
	case store.ChannelTelegram:
		return ch.ChannelType, w.sendTelegram(ctx, ch.Target, secret, item.BodyText, item.HTMLURL, item.ParseMode)
	case store.ChannelHTTPWebhook:
		return ch.ChannelType, w.sendHTTP(ctx, ch, secret, item)
	default:
		return ch.ChannelType, fmt.Errorf("unknown_channel")
	}
}

func (w *Worker) decryptSecret(ctx context.Context, envelope string) (string, error) {
	if envelope == "" {
		return "", nil
	}
	if w.KeyRing == nil {
		return "", fmt.Errorf("missing_keyring")
	}
	res, err := w.KeyRing.Decrypt(ctx, envelope, []byte(w.AAD))
	if err != nil {
		return "", err
	}
	return string(res.Plaintext), nil
}

func (w *Worker) sendTelegram(ctx context.Context, chatID, token, text, htmlURL, parseMode string) error {
	if token == "" || chatID == "" {
		return fmt.Errorf("telegram_not_configured")
	}
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	return w.sendTelegramDirect(ctx, api, chatID, token, text, htmlURL, parseMode)
}

// sendTelegramDirect 发送 Telegram 消息，api 参数为完整 URL，便于测试时替换端点。
// parseMode 为空时回退 HTML（既有正文均按 HTML 生成）。
func (w *Worker) sendTelegramDirect(ctx context.Context, api, chatID, token, text, htmlURL, parseMode string) error {
	if parseMode == "" {
		parseMode = "HTML"
	}
	text = truncateTelegramText(text)
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               parseMode,
		"disable_web_page_preview": true,
	}
	// 有 GitHub 链接时附加 inline keyboard 按钮
	if htmlURL != "" {
		payload["reply_markup"] = map[string]any{
			"inline_keyboard": [][]map[string]string{
				{{"text": "🔗 在 GitHub 中查看", "url": htmlURL}},
			},
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		// 先读限流响应体，再从中解析 retry_after 字段。
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, bodyDetailLimit))
		var tgResp struct {
			Parameters struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		if json.Unmarshal(raw, &tgResp) == nil && tgResp.Parameters.RetryAfter > 0 {
			return &retryAfterError{seconds: tgResp.Parameters.RetryAfter, code: "telegram_rate_limited"}
		}
		return &retryAfterError{seconds: 30, code: "telegram_rate_limited"}
	}
	// 4xx/5xx 读取响应体详情（截断），Telegram 会直接给出「chat not found」等可行动原因，
	// 带进错误后日志与 Outbox 排障无需再抓包。
	detail := readBodyDetail(resp)
	if resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 425 {
		return deliveryErrorf(fmt.Sprintf("telegram_http_%d", resp.StatusCode), detail)
	}
	if resp.StatusCode >= 400 {
		return deliveryErrorf(fmt.Sprintf("telegram_client_error_%d", resp.StatusCode), detail)
	}
	return nil
}

func (w *Worker) sendHTTP(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	if err := validateWebhookURL(ch.Target, ch.AllowPrivate); err != nil {
		return err
	}
	now := time.Now().UTC()
	payload := map[string]any{
		"spec_version": "1",
		"delivery_id":  item.ID,
		"emitted_at":   now.Format(time.RFC3339),
		"event":        item.BodyJSON,
		"title":        item.Title,
		// body_text 为 Telegram HTML 格式正文（保留兼容）；body_plain 为同一内容的纯文本，
		// 供不具备 HTML 解析能力的接收端直接消费。
		"body_text":  item.BodyText,
		"body_plain": htmlToPlainText(item.BodyText),
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.Target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	ts := now.Format(time.RFC3339)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Monitor-Event", "notification")
	req.Header.Set("X-GitHub-Monitor-Delivery", item.ID)
	req.Header.Set("X-GitHub-Monitor-Timestamp", ts)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		req.Header.Set("X-GitHub-Monitor-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
		// 429/503 等响应携带 Retry-After 时优先遵循上游退避指引，否则按固定阶梯重试。
		if ra := parseRetryAfter(resp); ra > 0 {
			return &retryAfterError{seconds: ra, code: "http_webhook_retry_after"}
		}
		return deliveryErrorf(fmt.Sprintf("http_webhook_status_%d", resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode >= 400 {
		return deliveryErrorf(fmt.Sprintf("http_webhook_client_%d", resp.StatusCode), readBodyDetail(resp))
	}
	return nil
}

func (w *Worker) handleFailure(ctx context.Context, item store.NotificationOutbox, err error) {
	code := deliveryErrorCode(err)
	var ra *retryAfterError
	if errors.As(err, &ra) {
		// 限流退避同样受重试上限约束，否则目标长期 429 时条目会无限重投。
		if item.AttemptCount >= maxAttempts {
			w.markDead(ctx, item.ID, code)
			return
		}
		if err := w.Store.Outbox().MarkRetry(ctx, item.ID, time.Now().UTC().Add(time.Duration(ra.seconds)*time.Second), code); err != nil && w.Logger != nil {
			// 重试时间写失败会让条目停留 sending 直到锁超时，记日志便于排查。
			w.Logger.Error("outbox retry mark failed", "outbox_id", item.ID, "error_code", "outbox_mark_failed", "error", err.Error())
		}
		return
	}
	if item.AttemptCount >= maxAttempts {
		w.markDead(ctx, item.ID, code)
		return
	}
	idx := item.AttemptCount - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(defaultBackoff) {
		idx = len(defaultBackoff) - 1
	}
	if err := w.Store.Outbox().MarkRetry(ctx, item.ID, time.Now().UTC().Add(defaultBackoff[idx]), code); err != nil && w.Logger != nil {
		w.Logger.Error("outbox retry mark failed", "outbox_id", item.ID, "error_code", "outbox_mark_failed", "error", err.Error())
	}
}

// markDead 将条目转入死信并触发指标回调。
func (w *Worker) markDead(ctx context.Context, id, code string) {
	if err := w.Store.Outbox().MarkDead(ctx, id, code); err != nil && w.Logger != nil {
		// 死信写失败会让条目无限重投：必须记录，便于人工介入。
		w.Logger.Error("outbox dead mark failed", "outbox_id", id, "error_code", "outbox_mark_failed", "error", err.Error())
	}
	if w.OnDead != nil {
		w.OnDead()
	}
}

type retryAfterError struct {
	seconds int
	code    string
}

// bodyDetailLimit 是错误详情携带的响应体上限：足够容纳 Telegram/接收端
// 一行可行动原因（如 chat not found），又不会把整段 HTML 错误页写进日志。
const bodyDetailLimit = 512

// logTitleLimit 是日志中 title 字段的上限：通知标题可能携带超长正文摘要，
// 全量写入会让单行日志膨胀，截断保留可读前缀即可。
const logTitleLimit = 120

// truncateLogTitle 按码点截断日志标题并追加省略号，避免超长标题刷屏。
func truncateLogTitle(title string) string {
	runes := []rune(title)
	if len(runes) <= logTitleLimit {
		return title
	}
	return string(runes[:logTitleLimit]) + "…"
}

// readBodyDetail 读取响应体前 bodyDetailLimit 字节并压缩为单行文本；
// 读取失败或内容为空时返回空串，由调用方决定是否拼接。
func readBodyDetail(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyDetailLimit))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	// 压缩连续空白与控制字符，避免换行/ANSI 序列污染 JSON 日志单行。
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > bodyDetailLimit {
		text = text[:bodyDetailLimit] + "…"
	}
	return text
}

// deliveryErrorf 组装投递错误：错误串以稳定错误码开头（如 telegram_client_error_400），
// 有响应体详情时追加「: 详情」，便于日志阅读与 deliveryErrorCode 前缀提取。
func deliveryErrorf(code, detail string) error {
	if detail == "" {
		return errors.New(code)
	}
	return fmt.Errorf("%s: %s", code, detail)
}

// deliveryCodeRe 限定错误码风格：小写字母开头、仅含小写字母/数字/下划线，
// 用于从错误串中提取稳定前缀，防止响应体详情等长文本写入 last_error_code。
var deliveryCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// deliveryErrorCode 从投递错误推导稳定的错误码（写入 outbox.last_error_code，
// 管理台「投递记录」页直接展示）：retryAfterError 用自带 code；其余错误取
// 错误串冒号前的稳定前缀，非代码风格一律归为 delivery_failed。
func deliveryErrorCode(err error) string {
	var ra *retryAfterError
	if errors.As(err, &ra) {
		return ra.code
	}
	s := err.Error()
	if i := strings.Index(s, ": "); i > 0 {
		s = s[:i]
	}
	if deliveryCodeRe.MatchString(s) {
		return s
	}
	return "delivery_failed"
}

// telegramTextLimit 是 Telegram 消息正文的保守上限。
// 官方限制为 4096 字符（HTML 模式下按解析后文本计算），留出余量避免长链接展开等边界情况。
const telegramTextLimit = 4000

// htmlLinkRe / htmlTagRe 用于把 Telegram HTML 正文转为纯文本（HTTP Webhook 接收端消费）。
var (
	htmlLinkRe = regexp.MustCompile(`<a href="([^"]*)">([^<]*)</a>`)
	htmlTagRe  = regexp.MustCompile(`<[^>]+>`)
)

// htmlToPlainText 将通知正文（HTML 格式）转为纯文本：
// 链接标签保留「文字 (URL)」便于接收端直接阅读，其余标签（<b>/<code> 等）剔除，
// HTML 实体（&amp; 等）反转义。仅作展示降级，不影响原 HTML 正文投递。
func htmlToPlainText(s string) string {
	s = htmlLinkRe.ReplaceAllString(s, `$2 ($1)`)
	s = htmlTagRe.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// truncateTelegramText 将超长消息安全截断到 telegramTextLimit 个字符：
// 按 Unicode 码点截断避免切断多字节字符；截断点若落在 HTML 标签或实体中间，
// 回退到最近的完整位置，避免把残缺标签/实体发给 Telegram 触发 400。
// 超长场景（AI 摘要、聚合消息、报告）不会因此进入死信。
func truncateTelegramText(text string) string {
	runes := []rune(text)
	if len(runes) <= telegramTextLimit {
		return text
	}
	s := string(runes[:telegramTextLimit])
	// 截断点落在开标签中间（如 <a href="..."）时回退到标签开始之前。
	if lastOpen := strings.LastIndex(s, "<"); lastOpen >= 0 {
		if lastClose := strings.LastIndex(s, ">"); lastClose < lastOpen {
			s = s[:lastOpen]
		}
	}
	// 剔除结尾残留的不完整 HTML 实体（如 &amp 无分号）。
	if amp := strings.LastIndex(s, "&"); amp >= 0 {
		rest := s[amp+1:]
		if rest != "" && !strings.Contains(rest, ";") && isEntityTail(rest) {
			s = s[:amp]
		}
	}
	s = strings.TrimRight(s, " \n")
	return s + "…\n（内容过长，已截断）"
}

// isEntityTail 判断字符串是否全为 HTML 实体字符（字母/数字/#），用于识别未闭合实体。
func isEntityTail(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '#' {
			continue
		}
		return false
	}
	return true
}

func (e *retryAfterError) Error() string { return e.code }

// maxRetryAfter 采纳 Retry-After 的上限：外部端点可能返回异常大的值，
// 上限 1 小时避免通知被长期搁置（重试次数仍受 maxAttempts 约束）。
const maxRetryAfter = 3600

// parseRetryAfter 解析 HTTP 响应的 Retry-After 响应头，支持整数秒与 HTTP 日期两种格式。
// 头缺失、格式非法或日期已过期时返回 0，由调用方走固定退避阶梯。
func parseRetryAfter(resp *http.Response) int {
	h := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > maxRetryAfter {
			return maxRetryAfter
		}
		return secs
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		secs := int(math.Ceil(d.Seconds()))
		if secs > maxRetryAfter {
			return maxRetryAfter
		}
		return secs
	}
	return 0
}

func validateWebhookURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid_webhook_url")
	}
	host := u.Hostname()
	lower := strings.ToLower(host)
	if host == "metadata.google.internal" || host == "169.254.169.254" || host == "metadata" ||
		lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("ssrf_blocked")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("private_target_blocked")
		}
		return nil
	}
	// 解析主机名，拦截解析到私网/链路本地的目标。
	addrs, err := net.LookupIP(host)
	if err != nil {
		// 无法解析时拒绝，避免把判断推迟到连接阶段。
		return fmt.Errorf("webhook_dns_lookup_failed")
	}
	if len(addrs) == 0 {
		return fmt.Errorf("webhook_dns_lookup_failed")
	}
	for _, ip := range addrs {
		if isBlockedIP(ip) {
			return fmt.Errorf("private_target_blocked")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// 额外拦截部分云元数据常见前缀（IPv4）。
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
	}
	return false
}

// newSafeHTTPClient 禁止跟随重定向，并在拨号时 pin 到校验过的公网 IP，降低 DNS rebinding 风险。
func newSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// 字面量 IP：直接二次校验。
			if ip := net.ParseIP(host); ip != nil {
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("private_target_blocked")
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook_dns_lookup_failed: %w", err)
			}
			var last error
			for _, a := range addrs {
				if isBlockedIP(a.IP) {
					last = fmt.Errorf("private_target_blocked")
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last == nil {
				last = fmt.Errorf("webhook_dns_lookup_failed")
			}
			return nil, last
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// 禁止跟随重定向，避免 30x 跳到内网/元数据地址绕过 SSRF 检查。
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
