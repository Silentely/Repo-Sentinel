package webhooksvc

import (
	"testing"
	"time"
)

func TestValidateWebhookTimestamp(t *testing.T) {
	now := time.Now()

	// 1. 正常时间戳（2 分钟前）
	if !ValidateWebhookTimestamp(now.Add(-2*time.Minute), now) {
		t.Fatal("2 分钟前的时间戳应判定为有效")
	}

	// 2. 超时重放（20 分钟前）
	if ValidateWebhookTimestamp(now.Add(-20*time.Minute), now) {
		t.Fatal("20 分钟前的时间戳超出 15 分钟上限，应判定为无效重放")
	}

	// 3. 未来时钟严重漂移（未来 30 分钟）
	if ValidateWebhookTimestamp(now.Add(30*time.Minute), now) {
		t.Fatal("未来 30 分钟的时间戳应判定为无效漂移")
	}

	// 4. 空时间戳放行
	if !ValidateWebhookTimestamp(time.Time{}, now) {
		t.Fatal("零值时间戳应安全放行")
	}
}
