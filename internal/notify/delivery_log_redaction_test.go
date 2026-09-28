package notify

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

// TestLogSafeDeliveryErrorStripsCredential 回归：投递失败日志不得写出携带令牌的目标 URL。
// Webhook URL（Discord/飞书/钉钉/企业微信/Bark）常把可发布消息的令牌放在 path 或 query，
// 而 Go 的 *url.Error 会原样带出完整 URL；直接写日志会把凭据落盘或送进日志聚合平台，
// 持有日志读权限者可凭该凭据向渠道推送任意消息（伪装告警、钓鱼）。
func TestLogSafeDeliveryErrorStripsCredential(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"discord path token", "https://hooks.example.com/api/webhooks/111/S3KR3T-WEBHOOK-TOKEN"},
		{"query token", "https://open.example.com/bot?key=SECRET-KEY&x=1"},
		{"userinfo", "https://user:pass@hooks.example.com/api/webhooks/111/TOKEN"},
	}
	for _, tc := range cases {
		got := logSafeDeliveryError(&url.Error{
			Op: "Post", URL: tc.raw, Err: errors.New("simulated transport failure"),
		})
		for _, secret := range []string{"S3KR3T", "SECRET-KEY", "TOKEN", "user:pass", "/api/webhooks/", "?key="} {
			if strings.Contains(got, secret) {
				t.Fatalf("%s：日志仍含凭据段 %q: %s", tc.name, secret, got)
			}
		}
		if !strings.Contains(got, "hooks.example.com") && !strings.Contains(got, "open.example.com") {
			t.Fatalf("%s：应保留 host 便于定位: %s", tc.name, got)
		}
		if !strings.Contains(got, "simulated transport failure") {
			t.Fatalf("%s：应保留协议层原因: %s", tc.name, got)
		}
	}

	// 非 url.Error 原样返回。
	if got := logSafeDeliveryError(errors.New("channel_not_found")); got != "channel_not_found" {
		t.Fatalf("非 url.Error 应原样返回，实际 %q", got)
	}
}

// TestDeliveryFailureLogOmitsTargetURL 端到端：按 Worker 的日志字段组组装一行，
// 确认不含目标 URL 凭据但保留定位所需字段。
func TestDeliveryFailureLogOmitsTargetURL(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	err := &url.Error{
		Op: "Post", URL: "https://93.184.216.34/api/webhooks/111/S3KR3T-WEBHOOK-TOKEN",
		Err: errors.New("boom"),
	}
	logger.Warn("notification delivery failed",
		"outbox_id", "ob-1", "channel_id", "ch-1", "channel_type", "http_webhook",
		"attempt", 1, "error_code", deliveryErrorCode(err), "error", logSafeDeliveryError(err))
	line := logs.String()
	if strings.Contains(line, "S3KR3T-WEBHOOK-TOKEN") || strings.Contains(line, "/api/webhooks/") {
		t.Fatalf("日志行含凭据: %s", line)
	}
	for _, want := range []string{"93.184.216.34", "ob-1", "ch-1", "http_webhook"} {
		if !strings.Contains(line, want) {
			t.Fatalf("日志行应保留定位字段 %q: %s", want, line)
		}
	}
}
