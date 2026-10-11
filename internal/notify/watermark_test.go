package notify

import (
	"strings"
	"testing"
)

func TestCheckOutboxWatermark(t *testing.T) {
	// 1. 正常低水位
	low := CheckOutboxWatermark(10, 0)
	if !strings.Contains(low.Level, "健康畅通") || low.BackpressureActive {
		t.Fatalf("低水位不应激活背压: %+v", low)
	}

	// 2. 超高水位 600 条
	high := CheckOutboxWatermark(600, 5)
	if !high.BackpressureActive || !strings.Contains(high.Level, "严重积压") {
		t.Fatalf("超高水位应激活背压提示: %+v", high)
	}
}
