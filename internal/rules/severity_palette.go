package rules

// SeverityLevel 统一事件严重度等级
type SeverityLevel string

const (
	SeverityCritical SeverityLevel = "critical"
	SeverityHigh     SeverityLevel = "high"
	SeverityMedium   SeverityLevel = "medium"
	SeverityLow      SeverityLevel = "low"
	SeverityInfo     SeverityLevel = "info"
)

// ChannelColorConfig 跨多渠道的颜色与视觉风格适配配置
type ChannelColorConfig struct {
	Level          SeverityLevel `json:"level"`
	BadgeEmoji     string        `json:"badge_emoji"`
	SlackHex       string        `json:"slack_hex"`
	FeishuTemplate string        `json:"feishu_template"` // red, orange, yellow, green, blue, grey
	WeComColor     string        `json:"wecom_color"`     // warning, comment, info
}

var paletteTable = map[SeverityLevel]ChannelColorConfig{
	SeverityCritical: {
		Level:          SeverityCritical,
		BadgeEmoji:     "🔴",
		SlackHex:       "#E01E5A",
		FeishuTemplate: "red",
		WeComColor:     "warning",
	},
	SeverityHigh: {
		Level:          SeverityHigh,
		BadgeEmoji:     "🟠",
		SlackHex:       "#E8912D",
		FeishuTemplate: "orange",
		WeComColor:     "warning",
	},
	SeverityMedium: {
		Level:          SeverityMedium,
		BadgeEmoji:     "🟡",
		SlackHex:       "#ECB22E",
		FeishuTemplate: "yellow",
		WeComColor:     "comment",
	},
	SeverityLow: {
		Level:          SeverityLow,
		BadgeEmoji:     "🔵",
		SlackHex:       "#2EB67D",
		FeishuTemplate: "blue",
		WeComColor:     "info",
	},
	SeverityInfo: {
		Level:          SeverityInfo,
		BadgeEmoji:     "⚪",
		SlackHex:       "#808080",
		FeishuTemplate: "grey",
		WeComColor:     "comment",
	},
}

// ResolveSeverityPalette 获取指定等级的多渠道调色映射
func ResolveSeverityPalette(level SeverityLevel) ChannelColorConfig {
	if cfg, ok := paletteTable[level]; ok {
		return cfg
	}
	return paletteTable[SeverityInfo]
}
