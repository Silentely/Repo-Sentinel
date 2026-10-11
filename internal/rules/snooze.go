package rules

import (
	"time"
)

// ExtendMuteExpiry 计算临时免打扰/静默窗口顺延后的到期时间点。
func ExtendMuteExpiry(currentExpiry, now time.Time, duration time.Duration) time.Time {
	if duration <= 0 {
		duration = time.Hour
	}

	if currentExpiry.IsZero() || currentExpiry.Before(now) {
		return now.Add(duration)
	}

	return currentExpiry.Add(duration)
}
