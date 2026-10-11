package notify

import (
	"strings"
	"testing"
)

func TestRedactSensitiveSecrets(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "GitHub PAT",
			input:    "构建失败，Token: ghp_1234567890abcdef1234567890abcdef12",
			contains: "ghp_***",
			excludes: "ghp_1234567890abcdef",
		},
		{
			name:     "OpenAI Key",
			input:    "API 报错 sk-abc1234567890abcdef1234567890",
			contains: "sk-***",
			excludes: "sk-abc1234567890",
		},
		{
			name:     "AWS Access Key",
			input:    "配置 AKIAIOSFODNN7EXAMPLE 异常",
			contains: "AKIA***",
			excludes: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name:     "Private Key",
			input:    "证书损坏 -----BEGIN RSA PRIVATE KEY----- MIIEowIBAAKCAQEA...",
			contains: "[REDACTED_PRIVATE_KEY]",
			excludes: "BEGIN RSA PRIVATE KEY",
		},
		{
			name:     "Private Key Body",
			input:    "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA7xSecretMaterial\n-----END RSA PRIVATE KEY-----",
			contains: "[REDACTED_PRIVATE_KEY]",
			excludes: "MIIEowIBAAKCAQEA7xSecretMaterial",
		},
		{
			name:     "Authorization Header",
			input:    "headers: Authorization: Bearer ghp_1234567890abcdef1234567890abcdef12",
			contains: "Authorization: Bearer ***",
			excludes: "ghp_1234567890abcdef1234567890abcdef12",
		},
		{
			name:     "Slack Webhook URL",
			input:    "投递目标 https://hooks.slack.com/services/T00000000/B00000000/SuperSecretPath",
			contains: "https://hooks.slack.com/services/***",
			excludes: "SuperSecretPath",
		},
		{
			name:     "URL Query Credential",
			input:    "回调 https://example.com/hook?token=abc123secretvalue&page=2",
			contains: "token=***",
			excludes: "abc123secretvalue",
		},
		{
			// 缺少 END 标记时只吃掉 base64 正文，后续正文不能被一起吞掉。
			name:     "Truncated Private Key",
			input:    "日志 -----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA7xSecretMaterial 后续正文保留",
			contains: "后续正文保留",
			excludes: "MIIEowIBAAKCAQEA7xSecretMaterial",
		},
		{
			// 普通查询参数不属于凭据，不能被误掩码。
			name:     "NonCredentialQueryParam",
			input:    "下载 https://example.com/file?key=download&page=2",
			contains: "key=download",
			excludes: "key=***",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := RedactSensitiveSecrets(tc.input)
			if !strings.Contains(output, tc.contains) {
				t.Fatalf("输出应包含掩码 %q，实际: %s", tc.contains, output)
			}
			if strings.Contains(output, tc.excludes) {
				t.Fatalf("输出不应包含敏感凭据 %q，实际: %s", tc.excludes, output)
			}
		})
	}
}
