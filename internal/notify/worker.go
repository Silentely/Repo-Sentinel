package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

var defaultBackoff = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

const maxAttempts = 8

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
		w.AAD = "reposentinel:notify-secret:v1"
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
			w.Logger.Error("outbox claim failed", "error_code", "database_unavailable")
		}
		return
	}
	for _, item := range items {
		if err := w.deliver(ctx, item); err != nil {
			w.handleFailure(ctx, item, err)
			continue
		}
		_ = w.Store.Outbox().MarkSent(ctx, item.ID)
		if w.OnSent != nil {
			w.OnSent()
		}
	}
}

func (w *Worker) deliver(ctx context.Context, item store.NotificationOutbox) error {
	ch, err := w.Store.Channels().Get(ctx, item.ChannelID)
	if err != nil {
		return err
	}
	secret, err := w.decryptSecret(ctx, ch.SecretEnvelope)
	if err != nil {
		return fmt.Errorf("decrypt_secret: %w", err)
	}
	switch ch.ChannelType {
	case store.ChannelTelegram:
		return w.sendTelegram(ctx, ch.Target, secret, item.BodyText)
	case store.ChannelHTTPWebhook:
		return w.sendHTTP(ctx, ch, secret, item)
	default:
		return fmt.Errorf("unknown_channel")
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

func (w *Worker) sendTelegram(ctx context.Context, chatID, token, text string) error {
	if token == "" || chatID == "" {
		return fmt.Errorf("telegram_not_configured")
	}
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	body, _ := json.Marshal(map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
		"disable_web_page_preview": true,
	})
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
		return &retryAfterError{seconds: 30, code: "telegram_rate_limited"}
	}
	if resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 425 {
		return fmt.Errorf("telegram_http_%d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		// 不回传响应体，避免把上游错误详情写入日志。
		return fmt.Errorf("telegram_client_error_%d", resp.StatusCode)
	}
	return nil
}

func (w *Worker) sendHTTP(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	if err := validateWebhookURL(ch.Target, ch.AllowPrivate); err != nil {
		return err
	}
	payload := map[string]any{
		"spec_version": "1",
		"delivery_id":  item.ID,
		"emitted_at":   time.Now().UTC().Format(time.RFC3339),
		"event":        item.BodyJSON,
		"title":        item.Title,
		"body_text":    item.BodyText,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.Target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
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
		return fmt.Errorf("http_webhook_status_%d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http_webhook_client_%d", resp.StatusCode)
	}
	return nil
}

func (w *Worker) handleFailure(ctx context.Context, item store.NotificationOutbox, err error) {
	code := "delivery_failed"
	if ra, ok := err.(*retryAfterError); ok {
		code = ra.code
		_ = w.Store.Outbox().MarkRetry(ctx, item.ID, time.Now().UTC().Add(time.Duration(ra.seconds)*time.Second), code)
		return
	}
	if item.AttemptCount >= maxAttempts {
		_ = w.Store.Outbox().MarkDead(ctx, item.ID, code)
		if w.OnDead != nil {
			w.OnDead()
		}
		return
	}
	idx := item.AttemptCount - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(defaultBackoff) {
		idx = len(defaultBackoff) - 1
	}
	_ = w.Store.Outbox().MarkRetry(ctx, item.ID, time.Now().UTC().Add(defaultBackoff[idx]), code)
}

type retryAfterError struct {
	seconds int
	code    string
}

func (e *retryAfterError) Error() string { return e.code }

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
