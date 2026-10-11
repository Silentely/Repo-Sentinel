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
