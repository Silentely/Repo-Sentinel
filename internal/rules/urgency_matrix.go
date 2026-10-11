package rules

import (
	"strings"
)

// UrgencyDecision 漏洞处置紧急度决策。
type UrgencyDecision struct {
	Priority    string `json:"priority"`
	ActionLabel string `json:"action_label"`
}

// DetermineVulnerabilityUrgency 结合严重等级、CVSS 分数与远程可利用性综合计算处置优先级。
func DetermineVulnerabilityUrgency(severity string, cvss float64, remotelyExploitable bool) UrgencyDecision {
	sev := strings.ToLower(strings.TrimSpace(severity))

	if cvss >= 9.0 || (sev == "critical" && remotelyExploitable) {
		return UrgencyDecision{
			Priority:    "P0",
			ActionLabel: "🚨 顶格致命漏洞：需立即升级或阻断网络暴露面",
		}
	}

	if cvss >= 7.0 || sev == "critical" || sev == "high" {
		return UrgencyDecision{
			Priority:    "P1",
			ActionLabel: "⚠️ 高危漏洞：建议 24 小时内完成版本升级或打补丁",
		}
	}

	if cvss >= 4.0 || sev == "medium" {
		return UrgencyDecision{
			Priority:    "P2",
			ActionLabel: "🟡 中危漏洞：排入本迭代常规迭代发布",
		}
	}

	return UrgencyDecision{
		Priority:    "P3",
		ActionLabel: "ℹ️ 低危漏洞：关注后续安全版本推进",
	}
}
