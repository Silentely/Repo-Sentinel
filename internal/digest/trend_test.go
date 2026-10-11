package digest

import (
	"strings"
	"testing"
)

func TestCalculateMovingAverage(t *testing.T) {
	history := []int{20, 20, 20, 20, 20} // 均值 20

	// 1. 常态水平（21）
	normal := CalculateMovingAverage(history, 21)
	if !strings.Contains(normal.Trend, "常态水平") {
		t.Errorf("expected 常态水平, got: %s", normal.Trend)
	}

	// 2. 明显偏高（35，高于均值 75%）
	high := CalculateMovingAverage(history, 35)
	if !strings.Contains(high.Trend, "高于近期均值") {
		t.Errorf("expected 高于近期均值, got: %s", high.Trend)
	}

	// 3. 明显偏低（10，低于均值 50%）
	low := CalculateMovingAverage(history, 10)
	if !strings.Contains(low.Trend, "低于近期均值") {
		t.Errorf("expected 低于近期均值, got: %s", low.Trend)
	}
}
