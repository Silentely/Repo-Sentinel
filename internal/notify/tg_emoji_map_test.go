package notify

import (
	"testing"
)

func TestGetTelegramEventBadge(t *testing.T) {
	// PR 已合并
	if badge := GetTelegramEventBadge("pull_request", "closed", "merged"); badge != "🟣" {
		t.Errorf("expected 🟣 for merged PR, got %s", badge)
	}

	// CI 失败
	if badge := GetTelegramEventBadge("workflow_run", "completed", "failure"); badge != "🔴" {
		t.Errorf("expected 🔴 for failed workflow, got %s", badge)
	}

	// 发布 Release
	if badge := GetTelegramEventBadge("release", "published", ""); badge != "🎉" {
		t.Errorf("expected 🎉 for release, got %s", badge)
	}

	// 严重安全警报
	if badge := GetTelegramEventBadge("dependabot_alert", "created", "critical"); badge != "🚨" {
		t.Errorf("expected 🚨 for critical security alert, got %s", badge)
	}

	// 默认兜底
	if badge := GetTelegramEventBadge("unknown_event", "", ""); badge != "🔔" {
		t.Errorf("expected 🔔 for unknown event, got %s", badge)
	}
}
