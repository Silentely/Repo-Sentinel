package notify

import (
	"math"
	"time"
)

// CalculateCappedBackoff 计算具备严格上限与防溢出保护的重试退避时间
// attempt: 当前重试次数 (1-indexed)
// baseDuration: 初始退避基数 (如 1s)
// maxCap: 封顶时长 (如 5m)
func CalculateCappedBackoff(attempt int, baseDuration, maxCap time.Duration) time.Duration {
	if attempt <= 1 {
		return baseDuration
	}
	if baseDuration <= 0 {
		baseDuration = time.Second
	}
	if maxCap <= 0 {
		maxCap = 5 * time.Minute
	}

	// 限制指数上限为 30，避免 math.Pow 浮点数溢出
	exp := attempt - 1
	if exp > 30 {
		exp = 30
	}

	multiplier := math.Pow(2, float64(exp))
	backoffFloat := float64(baseDuration) * multiplier

	if backoffFloat > float64(maxCap) || math.IsInf(backoffFloat, 0) {
		return maxCap
	}

	return time.Duration(backoffFloat)
}
