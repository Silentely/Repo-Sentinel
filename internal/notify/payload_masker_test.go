package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskSensitiveJSON(t *testing.T) {
	rawJSON := `{
		"event": "push",
		"repository": {
			"name": "Repo-Sentinel",
			"webhook_secret": "my-ultra-secret-12345"
		},
		"headers": {
			"Authorization": "Bearer gh_token_abcdef123"
		},
		"users": [
			{"name": "alice", "api_key": "sec_9999"}
		]
	}`

	maskedBytes, err := MaskSensitiveJSON([]byte(rawJSON))
	if err != nil {
		t.Fatalf("MaskSensitiveJSON failed: %v", err)
	}

	maskedStr := string(maskedBytes)

	// 确认敏感明文已被抹除
	if strings.Contains(maskedStr, "my-ultra-secret-12345") {
		t.Errorf("secret was not redacted: %s", maskedStr)
	}
	if strings.Contains(maskedStr, "Bearer gh_token_abcdef123") {
		t.Errorf("token was not redacted: %s", maskedStr)
	}
	if strings.Contains(maskedStr, "sec_9999") {
		t.Errorf("api_key was not redacted: %s", maskedStr)
	}

	// 验证 JSON 结构依然合法且保留正常字段
	var parsed map[string]interface{}
	if err := json.Unmarshal(maskedBytes, &parsed); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if parsed["event"] != "push" {
		t.Errorf("normal event field corrupted: %v", parsed["event"])
	}
}

// TestMaskSensitiveJSONNamingStylesAndEmbeddedValues 覆盖键名风格与值内嵌凭据两类漏网场景。
func TestMaskSensitiveJSONNamingStylesAndEmbeddedValues(t *testing.T) {
	rawJSON := `{
		"privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----",
		"pwd": "hunter2",
		"note": "Authorization: Bearer ghp_1234567890abcdef1234567890abcdef12",
		"author": "alice",
		"keywords": "release,build"
	}`

	maskedBytes, err := MaskSensitiveJSON([]byte(rawJSON))
	if err != nil {
		t.Fatalf("MaskSensitiveJSON failed: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(maskedBytes, &parsed); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}

	for _, key := range []string{"privateKey", "pwd"} {
		if got, _ := parsed[key].(string); !strings.Contains(got, "REDACTED") {
			t.Errorf("camelCase/短名敏感键 %s 未被掩码，got: %v", key, parsed[key])
		}
	}
	if note, _ := parsed["note"].(string); strings.Contains(note, "ghp_1234567890abcdef1234567890abcdef12") {
		t.Errorf("值内嵌凭据未被兜底脱敏，got: %v", note)
	}
	// 反向保护：普通业务字段不能被过度掩码，否则外发事件载荷失去可用性。
	if parsed["author"] != "alice" {
		t.Errorf("普通字段 author 被误掩码: %v", parsed["author"])
	}
	if parsed["keywords"] != "release,build" {
		t.Errorf("普通字段 keywords 被误掩码: %v", parsed["keywords"])
	}
}
