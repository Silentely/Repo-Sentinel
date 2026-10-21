package rules

import (
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestJevRoutingNoBlackhole(t *testing.T) {
	original := &store.Event{Kind: store.WorkItemKindIssue, Severity: "low"}
	inferred := &store.Event{
		Kind:     store.WorkItemKindIssue,
		Severity: "high",
		PayloadSummary: map[string]any{
			"jev_routing": map[string]any{
				"incident_tag": "security",
			},
		},
	}
	// 静态配置的渠道只接收 ci 标签
	channels := []store.NotificationChannel{
		{ID: "ci", Enabled: true, MinSeverity: "low", IncidentTags: []string{"ci"}},
	}
	// 模型给出的标签是 security，与 ci 渠道标签不匹配，但如果全部失配，必须回退到符合静态条件的渠道，不能产生黑洞
	selected := selectNotificationChannels(channels, original, inferred, "o/r", "", true)
	if len(selected) != 1 || selected[0].ID != "ci" {
		t.Fatalf("模型无匹配必须回退静态渠道，得到: %+v", selected)
	}

	// 如果静态原本就零匹配（例如严重度过低），则保持零匹配
	originalUnmatched := &store.Event{Kind: store.WorkItemKindIssue, Severity: "low"}
	channelsHigh := []store.NotificationChannel{
		{ID: "high-only", Enabled: true, MinSeverity: "high", IncidentTags: []string{"ci"}},
	}
	selectedZero := selectNotificationChannels(channelsHigh, originalUnmatched, inferred, "o/r", "", true)
	if len(selectedZero) != 0 {
		t.Fatalf("静态原本零匹配时应保持零匹配，得到: %+v", selectedZero)
	}

	// 当 RoutingEnabled 为 false 时，忽略 IncidentTags 限制（旧路由语义）
	selectedDisabled := selectNotificationChannels(channels, original, inferred, "o/r", "", false)
	if len(selectedDisabled) != 1 || selectedDisabled[0].ID != "ci" {
		t.Fatalf("RoutingEnabled 为 false 时应放行静态匹配渠道: %+v", selectedDisabled)
	}
}
