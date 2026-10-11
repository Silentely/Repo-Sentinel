package rules

import (
	"testing"
	"time"
)

func TestExtendMuteExpiry(t *testing.T) {
	now := time.Now()

	// 1. 之前已过期，基于 now 顺延 1 小时
	past := now.Add(-10 * time.Minute)
	newExp := ExtendMuteExpiry(past, now, time.Hour)
	if newExp.Sub(now) < 59*time.Minute || newExp.Sub(now) > 61*time.Minute {
		t.Fatalf("已过期的静默应基于 now 延长 1 小时，got: %v", newExp)
	}

	// 2. 之前尚余 30 分钟，继续顺延 1 小时 -> 剩余 90 分钟
	future := now.Add(30 * time.Minute)
	stackedExp := ExtendMuteExpiry(future, now, time.Hour)
	if stackedExp.Sub(now) < 89*time.Minute || stackedExp.Sub(now) > 91*time.Minute {
		t.Fatalf("未过期的静默应在原有到期点继续叠加顺延，got: %v", stackedExp)
	}
}
