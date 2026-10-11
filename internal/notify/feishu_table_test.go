package notify

import (
	"strings"
	"testing"
)

func TestFormatMarkdownTableAsFeishuFields(t *testing.T) {
	headers := []string{"服务", "耗时", "状态"}
	rows := [][]string{
		{"auth-svc", "15ms", "🟢 正常"},
		{"order-svc", "220ms", "🟡 延迟高"},
	}

	result := FormatMarkdownTableAsFeishuFields(headers, rows)

	expectedLine1 := "• **服务**: auth-svc | **耗时**: 15ms | **状态**: 🟢 正常"
	expectedLine2 := "• **服务**: order-svc | **耗时**: 220ms | **状态**: 🟡 延迟高"

	if !strings.Contains(result, expectedLine1) {
		t.Errorf("line 1 mismatch, got:\n%s", result)
	}
	if !strings.Contains(result, expectedLine2) {
		t.Errorf("line 2 mismatch, got:\n%s", result)
	}
}

func TestFormatMarkdownTableAsFeishuFields_Empty(t *testing.T) {
	if FormatMarkdownTableAsFeishuFields(nil, nil) != "" {
		t.Errorf("expected empty string for nil rows")
	}
}
