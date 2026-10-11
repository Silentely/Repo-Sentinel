package notify

import (
	"strings"
	"testing"
)

func TestBuildSlackEmergencyAlertBlocks(t *testing.T) {
	blocks := BuildSlackEmergencyAlertBlocks("生产环境部署失败", "Pod CrashLoopBackOff", "https://k8s.example.com", true)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks (header, details, action), got %d", len(blocks))
	}

	// 检查 channel 呼叫
	hBlock := blocks[0]
	txtObj, ok := hBlock["text"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected text object in block 0")
	}
	txtVal, ok := txtObj["text"].(string)
	if !ok || !strings.Contains(txtVal, "<!channel>") || !strings.Contains(txtVal, "CRITICAL") {
		t.Errorf("expected channel call and critical title, got: %s", txtVal)
	}

	// 检查按钮 style 为 danger
	actBlock := blocks[2]
	elems, ok := actBlock["elements"].([]map[string]interface{})
	if !ok || len(elems) != 1 {
		t.Fatalf("expected 1 element in action block")
	}
	if elems[0]["style"] != "danger" || elems[0]["url"] != "https://k8s.example.com" {
		t.Errorf("unexpected action button: %+v", elems[0])
	}
}
