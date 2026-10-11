package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// CapabilityFeature 描述系统的一项能力及其当前运行状态。
type CapabilityFeature struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	// Status 取值为 active（已装配且可用）或 unavailable（未启用，例如无可用渠道、AI 未开启）。
	Status string `json:"status"`
}

// SystemCapabilitiesMatrix 描述当前构建与运行时的能力清单。
type SystemCapabilitiesMatrix struct {
	Version           string              `json:"version"`
	Commit            string              `json:"commit"`
	BuildChannel      string              `json:"build_channel"`
	EnabledChannels   []string            `json:"enabled_channels"`
	SupportedChannels []string            `json:"supported_channels"`
	Features          []CapabilityFeature `json:"features"`
	Timestamp         time.Time           `json:"timestamp"`
}

// SystemCapabilitiesInput 汇总构建信息与运行时开关，供能力清单判定状态。
type SystemCapabilitiesInput struct {
	BuildInfo       buildinfo.Info
	EnabledChannels []string
	AIEnabled       bool
	DigestEnabled   bool
}

// supportedChannelTypes 列出代码支持的全部通知渠道类型（与 store 渠道常量保持一致）。
var supportedChannelTypes = []string{
	store.ChannelTelegram,
	store.ChannelHTTPWebhook,
	store.ChannelSlack,
	store.ChannelFeishu,
	store.ChannelDingTalk,
	store.ChannelWeCom,
	store.ChannelDiscord,
	store.ChannelBark,
}

// BuildSystemCapabilitiesMatrix 组装能力矩阵：版本来自真实构建元数据，
// 每项能力的状态由运行时开关推导，避免出现「清单已声明、实际未生效」的失实描述。
func BuildSystemCapabilitiesMatrix(in SystemCapabilitiesInput) SystemCapabilitiesMatrix {
	channels := in.EnabledChannels
	if channels == nil {
		channels = []string{}
	}
	sort.Strings(channels)

	notifyStatus := "unavailable"
	if len(channels) > 0 {
		notifyStatus = "active"
	}
	aiStatus := "unavailable"
	if in.AIEnabled {
		aiStatus = "active"
	}
	digestStatus := "unavailable"
	if in.DigestEnabled {
		digestStatus = "active"
	}

	return SystemCapabilitiesMatrix{
		Version:           in.BuildInfo.Version,
		Commit:            in.BuildInfo.GitSHA,
		BuildChannel:      in.BuildInfo.BuildChannel,
		EnabledChannels:   channels,
		SupportedChannels: supportedChannelTypes,
		Features: []CapabilityFeature{
			{Name: "multichannel_delivery", Category: "notify", Description: "Telegram / 飞书 / 钉钉 / 企业微信 / Slack / Discord / Bark / HTTP Webhook 多渠道投递与渠道级过滤", Status: notifyStatus},
			{Name: "delivery_retry_backoff", Category: "notify", Description: "投递失败指数退避重试上限、渠道健康度追踪与死信一键重投", Status: notifyStatus},
			{Name: "quiet_hours", Category: "notify", Description: "渠道级免打扰时段与紧急安全告警穿透豁免", Status: notifyStatus},
			{Name: "digest_reports", Category: "digest", Description: "日/周/月定期摘要、活跃仓库排行与历史归档查询", Status: digestStatus},
			{Name: "ai_assistance", Category: "ai", Description: "PR 代码审查、Issue 智能分诊、安全告警分析与 CI 失败归因", Status: aiStatus},
			{Name: "chatops_actions", Category: "ops", Description: "Telegram / 飞书 交互按钮触发 Actions 工作流重跑，一次性 Token 防重放", Status: "active"},
			{Name: "webhook_integrity", Category: "security", Description: "GitHub Webhook HMAC-SHA256 验签、投递指纹去重与保留期清理", Status: "active"},
			{Name: "outbound_ssrf_guard", Category: "security", Description: "出站 Webhook 私网与云元数据地址拦截、禁止重定向跟随", Status: "active"},
		},
		Timestamp: time.Now().UTC(),
	}
}

// handleCapabilities 提供 GET /system/capabilities：返回真实构建版本与当前生效的能力清单。
func (s *server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	channels, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	enabled := make([]string, 0, len(channels))
	seen := make(map[string]bool, len(channels))
	for _, ch := range channels {
		if ch.Enabled && !seen[ch.ChannelType] {
			seen[ch.ChannelType] = true
			enabled = append(enabled, ch.ChannelType)
		}
	}
	writeJSON(w, http.StatusOK, BuildSystemCapabilitiesMatrix(SystemCapabilitiesInput{
		BuildInfo:       s.dependencies.BuildInfo,
		EnabledChannels: enabled,
		AIEnabled:       s.dependencies.Config.AI.Enabled,
		DigestEnabled:   s.dependencies.Config.AI.Enabled && s.dependencies.Config.AI.DigestEnabled,
	}))
}
