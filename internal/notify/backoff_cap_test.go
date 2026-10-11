package notify

import (
	"testing"
	"time"
)

func TestCalculateCappedBackoff(t *testing.T) {
	base := 2 * time.Second
	cap := 30 * time.Second

	// attempt 1: base = 2s
	b1 := CalculateCappedBackoff(1, base, cap)
	if b1 != 2*time.Second {
		t.Errorf("expected 2s for attempt 1, got %v", b1)
	}

	// attempt 2: 2s * 2 = 4s
	b2 := CalculateCappedBackoff(2, base, cap)
	if b2 != 4*time.Second {
		t.Errorf("expected 4s for attempt 2, got %v", b2)
	}

	// attempt 3: 2s * 4 = 8s
	b3 := CalculateCappedBackoff(3, base, cap)
	if b3 != 8*time.Second {
		t.Errorf("expected 8s for attempt 3, got %v", b3)
	}

	// attempt 10: 理论值 1024s，必须被 cap 限制在 30s
	b10 := CalculateCappedBackoff(10, base, cap)
	if b10 != 30*time.Second {
		t.Errorf("expected cap of 30s for attempt 10, got %v", b10)
	}

	// attempt 100: 超大值防溢出，必须仍然等于 30s
	b100 := CalculateCappedBackoff(100, base, cap)
	if b100 != 30*time.Second {
		t.Errorf("expected cap of 30s for huge attempt, got %v", b100)
	}
}
