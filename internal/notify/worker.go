package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
}

// Run 循环领取并发送。
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 15 * time.Second}
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram_client_error:%s", string(b))
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
	if host == "metadata.google.internal" || host == "169.254.169.254" || host == "metadata" {
		return fmt.Errorf("ssrf_blocked")
	}
	if !allowPrivate {
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
				return fmt.Errorf("private_target_blocked")
			}
		}
		lower := strings.ToLower(host)
		if lower == "localhost" || strings.HasSuffix(lower, ".local") {
			return fmt.Errorf("private_target_blocked")
		}
	}
	return nil
}
