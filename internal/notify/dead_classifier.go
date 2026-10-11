package notify

import (
	"strings"
)

// DeadCauseCategory 死信归因类型。
type DeadCauseCategory string

const (
	DeadCauseConfiguration DeadCauseCategory = "configuration" // 确定性配置/凭据错误
	DeadCauseTransient     DeadCauseCategory = "transient"     // 临时网络/限流/服务抖动
	DeadCausePayload       DeadCauseCategory = "payload"       // 内容过长/非法格式
	DeadCauseUnknown       DeadCauseCategory = "unknown"       // 未知原因
)

// ClassifyDeadLetter 对投递失败死信进行归因分类，并给出是否推荐一键重试的运维建议。
func ClassifyDeadLetter(errorCode string) (category DeadCauseCategory, retryRecommended bool, hint string) {
	code := strings.ToLower(strings.TrimSpace(errorCode))
	switch {
	case strings.Contains(code, "timeout"), strings.Contains(code, "connection_refused"),
		strings.Contains(code, "502"), strings.Contains(code, "503"), strings.Contains(code, "504"),
		strings.Contains(code, "rate_limited"), strings.Contains(code, "too_many_requests"):
		return DeadCauseTransient, true, "临时网络抖动或上游限流，推荐一键重新投递"

	case strings.Contains(code, "unauthorized"), strings.Contains(code, "forbidden"),
		strings.Contains(code, "not_found"), strings.Contains(code, "chat_not_found"),
		strings.Contains(code, "invalid_token"), strings.Contains(code, "missing_secret"):
		return DeadCauseConfiguration, false, "渠道目标或凭据配置失效，请核对渠道设置后再重试"

	case strings.Contains(code, "too_long"), strings.Contains(code, "entity_parse"),
		strings.Contains(code, "bad_request"):
		return DeadCausePayload, false, "消息内容过长或包含非法格式"

	default:
		return DeadCauseUnknown, true, "异常原因未归类，可尝试重新投递"
	}
}
