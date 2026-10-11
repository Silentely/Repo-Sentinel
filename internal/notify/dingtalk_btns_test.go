package notify

import (
	"testing"
)

func TestBuildDingTalkActionCardPayload(t *testing.T) {
	btns := []DingTalkBtn{
		{Title: "查看 PR", ActionURL: "https://github.com/org/repo/pull/1"},
		{Title: "在线合并", ActionURL: "https://github.com/org/repo/pull/1/files"},
	}

	payload := BuildDingTalkActionCardPayload("新 PR 提交通知", "### [repo] 新 PR 提交", btns, true)

	if payload["msgtype"] != "actionCard" {
		t.Fatalf("expected msgtype actionCard, got %v", payload["msgtype"])
	}

	ac, ok := payload["actionCard"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected actionCard object")
	}

	if ac["btnOrientation"] != "1" {
		t.Errorf("expected horizontal btnOrientation 1, got %v", ac["btnOrientation"])
	}

	btnList, ok := ac["btns"].([]map[string]string)
	if !ok || len(btnList) != 2 {
		t.Fatalf("expected 2 btns in list, got %+v", ac["btns"])
	}

	if btnList[0]["title"] != "查看 PR" || btnList[0]["actionURL"] != "https://github.com/org/repo/pull/1" {
		t.Errorf("unexpected btn[0]: %+v", btnList[0])
	}
}
