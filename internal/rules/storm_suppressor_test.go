package rules

import (
	"strings"
	"testing"
	"time"
)

func TestStormSuppressor(t *testing.T) {
	now := time.Now()
	// 5 分钟窗口，阈值 3 次
	suppressor := NewStormSuppressor(5*time.Minute, 3)

	fp := "ci_failure_backend"

	// 前 3 次不抑制
	for i := 1; i <= 3; i++ {
		res := suppressor.CheckAndRecord(fp, now.Add(time.Duration(i)*time.Second))
		if res.Suppressed {
			t.Fatalf("expected call %d to not be suppressed", i)
		}
		if res.TriggerCount != i {
			t.Errorf("expected count %d, got %d", i, res.TriggerCount)
		}
	}

	// 第 4 次应该被抑制
	res4 := suppressor.CheckAndRecord(fp, now.Add(4*time.Second))
	if !res4.Suppressed {
		t.Fatalf("expected 4th call to be suppressed")
	}
	if !strings.Contains(res4.StormWarning, "告警风暴") {
		t.Errorf("expected storm warning message, got %s", res4.StormWarning)
	}

	// 窗口过期后 (6 分钟后)，应该恢复
	resAfter := suppressor.CheckAndRecord(fp, now.Add(6*time.Minute))
	if resAfter.Suppressed {
		t.Fatalf("expected call after window expiration to not be suppressed")
	}
	if resAfter.TriggerCount != 1 {
		t.Errorf("expected count reset to 1, got %d", resAfter.TriggerCount)
	}
}
