package webhooksvc

import (
	"time"
)

const defaultMaxClockDrift = 15 * time.Minute

// ValidateWebhookTimestamp 校验 Webhook 事件时间戳是否处于合理的时间窗口内，防止重放攻击。
func ValidateWebhookTimestamp(eventTime, now time.Time, maxDrift ...time.Duration) bool {
	if eventTime.IsZero() {
		return true // 未提供时间戳时不阻断
	}

	driftLimit := defaultMaxClockDrift
	if len(maxDrift) > 0 && maxDrift[0] > 0 {
		driftLimit = maxDrift[0]
	}

	diff := now.Sub(eventTime)
	if diff < 0 {
		diff = -diff
	}

	return diff <= driftLimit
}
