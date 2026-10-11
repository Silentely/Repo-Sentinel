package notify

import (
	"strings"
	"testing"
)

func TestFormatIndentedBlock(t *testing.T) {
	title := "变更高风险项"
	lines := []string{
		"删除了生产数据库连接池",
		"• 修改了网关鉴权公钥",
		"调整了跨域 CORS 规则",
	}

	result := FormatIndentedBlock(title, lines, 2)
	if !strings.Contains(result, "<b>变更高风险项</b>") {
		t.Errorf("expected bold title, got:\n%s", result)
	}

	if !strings.Contains(result, "  • 删除了生产数据库连接池") {
		t.Errorf("expected bullet added with 2-space indent, got:\n%s", result)
	}

	if !strings.Contains(result, "  • 修改了网关鉴权公钥") {
		t.Errorf("expected preserved bullet with 2-space indent, got:\n%s", result)
	}
}

func TestFormatIndentedBlock_EmptyLines(t *testing.T) {
	result := FormatIndentedBlock("标题", nil, 2)
	if result != "<b>标题</b>" {
		t.Errorf("expected '<b>标题</b>', got %q", result)
	}
}
