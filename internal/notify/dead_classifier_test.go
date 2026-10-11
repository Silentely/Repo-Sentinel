package notify

import (
	"testing"
)

func TestClassifyDeadLetter(t *testing.T) {
	// 1. 瞬态抖动可恢复错误
	cat, retry, _ := ClassifyDeadLetter("network_timeout")
	if cat != DeadCauseTransient || !retry {
		t.Fatalf("network_timeout 应为 transient 且可重试，got: %v, %v", cat, retry)
	}

	// 2. 配置错误（不可重试）
	catCfg, retryCfg, _ := ClassifyDeadLetter("chat_not_found")
	if catCfg != DeadCauseConfiguration || retryCfg {
		t.Fatalf("chat_not_found 应为 configuration 且不建议重试，got: %v, %v", catCfg, retryCfg)
	}

	// 3. 内容过长错误
	catPayload, retryPayload, _ := ClassifyDeadLetter("message_too_long")
	if catPayload != DeadCausePayload || retryPayload {
		t.Fatalf("message_too_long 应为 payload 且不建议重试，got: %v, %v", catPayload, retryPayload)
	}
}
