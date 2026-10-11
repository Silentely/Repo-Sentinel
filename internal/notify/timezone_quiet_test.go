package notify

import (
	"testing"
	"time"
)

func TestEvaluateQuietHoursByTimezone(t *testing.T) {
	// 构造 2026-10-11 15:30:00 UTC
	// 在 UTC+8 (Asia/Shanghai) 为 2026-10-11 23:30:00 (处于 22:00 ~ 08:00 免打扰区间)
	// 在 UTC-7 (America/Los_Angeles) 为 2026-10-11 08:30:00 (不处于 22:00 ~ 08:00 区间)
	utcTime := time.Date(2026, 10, 11, 15, 30, 0, 0, time.UTC)

	// 测试上海时区
	shEval, err := EvaluateQuietHoursByTimezone(utcTime, "Asia/Shanghai", 22, 8)
	if err != nil {
		t.Fatalf("failed to evaluate Asia/Shanghai: %v", err)
	}
	if !shEval.InQuietHours {
		t.Errorf("expected InQuietHours to be true for 23:30 in Shanghai, got false")
	}
	// 23:30 到次日 08:00 剩余 8小时30分 = 510 分钟
	expectedRem := 8*time.Hour + 30*time.Minute
	if shEval.Remaining != expectedRem {
		t.Errorf("expected remaining %v, got %v", expectedRem, shEval.Remaining)
	}

	// 测试洛杉矶时区
	laEval, err := EvaluateQuietHoursByTimezone(utcTime, "America/Los_Angeles", 22, 8)
	if err != nil {
		t.Fatalf("failed to evaluate America/Los_Angeles: %v", err)
	}
	if laEval.InQuietHours {
		t.Errorf("expected InQuietHours to be false for 08:30 in LA, got true")
	}
}

func TestEvaluateQuietHoursByTimezone_InvalidTZ(t *testing.T) {
	_, err := EvaluateQuietHoursByTimezone(time.Now(), "Invalid/Zone_Name", 22, 8)
	if err == nil {
		t.Errorf("expected error for invalid timezone, got nil")
	}
}
