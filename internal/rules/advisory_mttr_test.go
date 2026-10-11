package rules

import (
	"strings"
	"testing"
	"time"
)

func TestCalculateRemediationDuration(t *testing.T) {
	now := time.Now()

	// 1. 2 小时黄金修复
	fast := CalculateRemediationDuration(now.Add(-2*time.Hour), now)
	if !strings.Contains(fast.Badge, "黄金响应") {
		t.Fatalf("2 小时修复应为黄金响应，got: %s", fast.Badge)
	}

	// 2. 48 小时正常修复
	medium := CalculateRemediationDuration(now.Add(-48*time.Hour), now)
	if !strings.Contains(medium.Badge, "敏捷修复") {
		t.Fatalf("48 小时修复应为敏捷修复，got: %s", medium.Badge)
	}

	// 3. 5 天偏长
	slow := CalculateRemediationDuration(now.Add(-120*time.Hour), now)
	if !strings.Contains(slow.Badge, "修复周期偏长") {
		t.Fatalf("5 天修复应为偏长，got: %s", slow.Badge)
	}
}
