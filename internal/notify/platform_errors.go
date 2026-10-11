package notify

import (
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// PlatformErrorExplanation 第三方开放平台错误码诊断释义。
type PlatformErrorExplanation struct {
	Code        string `json:"code"`
	Meaning     string `json:"meaning"`
	FixGuidance string `json:"fix_guidance"`
}

// InterpretPlatformError 将企微/飞书等第三方数字错误码转译为人性化排障建议。
func InterpretPlatformError(channelType, rawError string) PlatformErrorExplanation {
	errStr := strings.ToLower(rawError)

	switch channelType {
	case store.ChannelWeCom:
		switch {
		case strings.Contains(errStr, "93000"):
			return PlatformErrorExplanation{Code: "93000", Meaning: "机器人已被移出群聊或已被解散", FixGuidance: "请将机器人重新拉入目标群组，或更新 Webhook 地址"}
		case strings.Contains(errStr, "45009"):
			return PlatformErrorExplanation{Code: "45009", Meaning: "企业微信接口调用频率触发官方阈值", FixGuidance: "消息已进入平滑退避队列，稍后将自动重试"}
		case strings.Contains(errStr, "40058"):
			return PlatformErrorExplanation{Code: "40058", Meaning: "Markdown 消息内容参数非法或超长", FixGuidance: "检查是否发送了过大的附件或非白名单 Markdown 标签"}
		}

	case store.ChannelFeishu:
		switch {
		case strings.Contains(errStr, "19001"), strings.Contains(errStr, "19002"):
			return PlatformErrorExplanation{Code: "19001", Meaning: "飞书机器人加签密钥验证失败", FixGuidance: "请在飞书机器人设置中核对加签 Secret 是否与配置一致"}
		case strings.Contains(errStr, "9499"):
			return PlatformErrorExplanation{Code: "9499", Meaning: "飞书卡片 JSON 请求体格式非法", FixGuidance: "检查卡片结构元素是否完整"}
		}
	}

	return PlatformErrorExplanation{
		Code:        "general",
		Meaning:     "未知服务商返回错误",
		FixGuidance: "核对请求目标 URL 与系统日志详细信息",
	}
}
