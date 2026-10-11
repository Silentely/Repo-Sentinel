package rules

import (
	"fmt"
	"sync"
	"time"
)

// StormSuppressionDecision 告警风暴判定决策
type StormSuppressionDecision struct {
	Suppressed    bool   `json:"suppressed"`     // 是否应该抑制单独发信
	TriggerCount  int    `json:"trigger_count"`  // 窗口内触发总次数
	StormWarning  string `json:"storm_warning"`  // 注入卡片的风暴告警提示
}

// StormSuppressor 内存滑动窗口告警风暴抑制器
type StormSuppressor struct {
	mu            sync.Mutex
	window        time.Duration
	threshold     int
	eventHistory  map[string][]time.Time
}

// NewStormSuppressor 创建风暴抑制器
// window: 滑动窗口时长 (例如 5 * time.Minute)
// threshold: 窗口内最大允许单独通知次数 (例如 5)
func NewStormSuppressor(window time.Duration, threshold int) *StormSuppressor {
	if window <= 0 {
		window = 5 * time.Minute
	}
	if threshold <= 0 {
		threshold = 5
	}
	return &StormSuppressor{
		window:       window,
		threshold:    threshold,
		eventHistory: make(map[string][]time.Time),
	}
}

// CheckAndRecord 记录事件并评估是否进入风暴抑制
func (s *StormSuppressor) CheckAndRecord(fingerprint string, now time.Time) StormSuppressionDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := now.Add(-s.window)
	timestamps := s.eventHistory[fingerprint]

	// 清理过期的时间戳
	var valid []time.Time
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	valid = append(valid, now)
	s.eventHistory[fingerprint] = valid
	count := len(valid)

	if count > s.threshold {
		return StormSuppressionDecision{
			Suppressed:   true,
			TriggerCount: count,
			StormWarning: fmt.Sprintf("⚠️ 监测到告警风暴：该告警在过去 %s 内已触发 %d 次，已进入聚合抑制模式", s.window.String(), count),
		}
	}

	return StormSuppressionDecision{
		Suppressed:   false,
		TriggerCount: count,
	}
}
