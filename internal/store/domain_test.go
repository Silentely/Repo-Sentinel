package store

import "testing"

// TestKindDisplayName 守护领域层事件类型中文名（通知正文与 AI 分诊共用）。
func TestKindDisplayName(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{WorkItemKindIssue, "Issue"},
		{WorkItemKindPR, "PR"},
		{AlertKindDependabot, "Dependabot 依赖告警"},
		{AlertKindCodeScanning, "Code Scanning 代码扫描"},
		{AlertKindSecretScanning, "Secret Scanning 密钥扫描"},
		{"workflow_run", "Actions 工作流"},
		{"custom_kind", "custom_kind"},
	}
	for _, tc := range cases {
		if got := KindDisplayName(tc.kind); got != tc.want {
			t.Errorf("KindDisplayName(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// TestPayloadString 守护 PayloadSummary 字符串读取：类型不符、nil、缺失键均返回空。
func TestPayloadString(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42, "slice": []any{"a"}}
	if got := PayloadString(m, "key"); got != "value" {
		t.Errorf("PayloadString 期望 value，实际 %q", got)
	}
	for _, key := range []string{"num", "slice", "missing"} {
		if got := PayloadString(m, key); got != "" {
			t.Errorf("PayloadString(%q) 应返回空，实际 %q", key, got)
		}
	}
	if got := PayloadString(nil, "key"); got != "" {
		t.Errorf("nil map 应返回空，实际 %q", got)
	}
}
