package notify

import (
	"testing"
)

func TestBuildFeishuCollapsibleElement(t *testing.T) {
	el := BuildFeishuCollapsibleElement("点击展开详情", "**详细说明**")
	if el["tag"] != "collapsible_panel" {
		t.Fatalf("expected collapsible_panel tag, got %v", el["tag"])
	}
	header, ok := el["header"].(map[string]any)
	if !ok {
		t.Fatalf("expected header object, got %v", el["header"])
	}
	title := header["title"].(map[string]any)["content"].(string)
	if title != "点击展开详情" {
		t.Fatalf("title mismatch: %s", title)
	}
}
