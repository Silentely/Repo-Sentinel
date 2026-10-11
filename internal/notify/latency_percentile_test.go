package notify

import (
	"testing"
	"time"
)

func TestCalculateLatencyPercentiles(t *testing.T) {
	// 生成 100 个耗时样本: 1ms 到 100ms
	var samples []time.Duration
	for i := 1; i <= 100; i++ {
		samples = append(samples, time.Duration(i)*time.Millisecond)
	}

	metrics := CalculateLatencyPercentiles(samples)

	if metrics.Count != 100 {
		t.Fatalf("expected count 100, got %d", metrics.Count)
	}
	if metrics.Min != 1*time.Millisecond {
		t.Errorf("expected min 1ms, got %v", metrics.Min)
	}
	if metrics.Max != 100*time.Millisecond {
		t.Errorf("expected max 100ms, got %v", metrics.Max)
	}
	if metrics.P50 != 50*time.Millisecond && metrics.P50 != 51*time.Millisecond {
		t.Errorf("expected P50 around 50ms, got %v", metrics.P50)
	}
	if metrics.P95 != 95*time.Millisecond && metrics.P95 != 96*time.Millisecond {
		t.Errorf("expected P95 around 95ms, got %v", metrics.P95)
	}
	if metrics.P99 != 99*time.Millisecond && metrics.P99 != 100*time.Millisecond {
		t.Errorf("expected P99 around 99ms, got %v", metrics.P99)
	}
}

func TestCalculateLatencyPercentiles_Empty(t *testing.T) {
	metrics := CalculateLatencyPercentiles(nil)
	if metrics.Count != 0 {
		t.Errorf("expected count 0, got %d", metrics.Count)
	}
}
