package rules

import (
	"testing"
)

func TestCalculateRepoHealth(t *testing.T) {
	// 1. 全绿满分
	perfect := CalculateRepoHealth(0, 0, 0)
	if perfect.Score != 100 || perfect.Grade != "A" {
		t.Fatalf("满分情况不符: %+v", perfect)
	}

	// 2. 一个高危告警扣 25 分 -> 75 分 Grade B
	withHigh := CalculateRepoHealth(1, 0, 0)
	if withHigh.Score != 75 || withHigh.Grade != "B" {
		t.Fatalf("高危告警计算不符: %+v", withHigh)
	}

	// 3. 严重超标扣到 0 分 -> Grade D
	worst := CalculateRepoHealth(5, 5, 5)
	if worst.Score != 0 || worst.Grade != "D" {
		t.Fatalf("严重扣分下限应为 0 分，got %+v", worst)
	}
}
