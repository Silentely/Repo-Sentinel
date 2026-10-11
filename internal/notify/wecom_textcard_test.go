package notify

import (
	"testing"
)

func TestBuildWeComTextCardPayload(t *testing.T) {
	p := BuildWeComTextCardPayload("构建失败", "详情描述", "https://github.com/test")
	if p["msgtype"] != "textcard" {
		t.Fatalf("expected textcard msgtype, got %v", p["msgtype"])
	}
	tc, ok := p["textcard"].(map[string]any)
	if !ok {
		t.Fatalf("expected textcard map, got %v", p["textcard"])
	}
	if tc["title"] != "构建失败" || tc["url"] != "https://github.com/test" {
		t.Fatalf("textcard fields mismatch: %+v", tc)
	}
}
