package notify

import (
	"testing"
)

func TestBuildFeishuPostFallbackPayload(t *testing.T) {
	title := "构建警报"
	paragraphs := []string{
		"仓库: org/repo",
		"分支: main",
		"失败步骤: test",
	}

	payload := BuildFeishuPostFallbackPayload(title, paragraphs)
	if payload["msg_type"] != "post" {
		t.Fatalf("expected msg_type post, got %v", payload["msg_type"])
	}

	content, ok := payload["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected content object")
	}

	post, ok := content["post"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected post object")
	}

	zhCn, ok := post["zh_cn"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected zh_cn object")
	}

	if zhCn["title"] != title {
		t.Errorf("expected title %s, got %v", title, zhCn["title"])
	}

	rows, ok := zhCn["content"].([][]map[string]string)
	if !ok || len(rows) != 3 {
		t.Fatalf("expected 3 content rows, got %+v", zhCn["content"])
	}
	if rows[0][0]["text"] != "仓库: org/repo" {
		t.Errorf("unexpected first row text: %s", rows[0][0]["text"])
	}
}
