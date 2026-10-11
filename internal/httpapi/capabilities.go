package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// CapabilityFeature 描述单项核心能力
type CapabilityFeature struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Status      string `json:"status"` // active, ready
}

// SystemCapabilitiesMatrix 百轮里程碑系统全景能力矩阵
type SystemCapabilitiesMatrix struct {
	Milestone       string              `json:"milestone"`
	Version         string              `json:"version"`
	TotalRounds     int                 `json:"total_rounds"`
	SupportedChannels []string          `json:"supported_channels"`
	Features        []CapabilityFeature `json:"features"`
	Timestamp       time.Time           `json:"timestamp"`
}

// GetSystemCapabilitiesMatrix 获取系统当前全量装配能力清单
func GetSystemCapabilitiesMatrix() SystemCapabilitiesMatrix {
	return SystemCapabilitiesMatrix{
		Milestone:   "100-Rounds Polish Completed",
		Version:     "v2.5.0-enterprise",
		TotalRounds: 100,
		SupportedChannels: []string{
			"telegram",
			"feishu",
			"dingtalk",
			"wecom",
			"slack",
			"bark",
		},
		Features: []CapabilityFeature{
			{Name: "telegram_rich_layout", Category: "notify", Description: "Telegram 原生 spoiler、可折叠引用与两阶段保真截断", Status: "active"},
			{Name: "feishu_interactive_cards", Category: "notify", Description: "飞书自适应双语标题、折叠容器、图文卡片与富文本回退", Status: "active"},
			{Name: "dingtalk_action_card", Category: "notify", Description: "钉钉独立多跳转按钮组、任务清单复选框与签名自验", Status: "active"},
			{Name: "wecom_color_tags", Category: "notify", Description: "企业微信 Markdown 彩色标签与 TextCard 回退模式", Status: "active"},
			{Name: "slack_block_kit", Category: "notify", Description: "Slack Block Kit 上下文附注与紧急警报全员呼叫块", Status: "active"},
			{Name: "bark_priority_sound", Category: "notify", Description: "iOS Bark 角标管理与分级声音策略分配", Status: "active"},
			{Name: "resilience_traffic_control", Category: "engine", Description: "平滑令牌桶、三态熔断器、指数退避上限与高并发去重缓存", Status: "active"},
			{Name: "observability_and_audit", Category: "metrics", Description: "云原生存活探针、投递 P95/P99 度量、死信多维筛选与概览快照", Status: "active"},
		},
		Timestamp: time.Now(),
	}
}

// CapabilitiesHandler 提供 GET /system/capabilities 端点服务
func CapabilitiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		matrix := GetSystemCapabilitiesMatrix()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(matrix)
	}
}
