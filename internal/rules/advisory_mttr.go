package rules

import (
	"fmt"
	"time"
)

// RemediationStats 漏洞平均修复耗时与敏捷评级。
type RemediationStats struct {
	Hours float64 `json:"hours"`
	Badge string  `json:"badge"`
}

// CalculateRemediationDuration 计算漏洞从触发告警到修复关闭的耗时与效率评级。
func CalculateRemediationDuration(openTime, fixTime time.Time) RemediationStats {
	if openTime.IsZero() || fixTime.IsZero() || fixTime.Before(openTime) {
		return RemediationStats{}
	}

	diff := fixTime.Sub(openTime)
	hours := diff.Hours()

	var badge string
	switch {
	case hours <= 24:
		badge = fmt.Sprintf("⚡ 黄金响应 (%.1f 小时内修复)", hours)
	case hours <= 72:
		badge = fmt.Sprintf("🟢 敏捷修复 (%.1f 天内完成)", hours/24)
	default:
		badge = fmt.Sprintf("⚠️ 修复周期偏长 (%.1f 天)", hours/24)
	}

	return RemediationStats{
		Hours: hours,
		Badge: badge,
	}
}
