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
