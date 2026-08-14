package rules

import (
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestAlertWithdrawnDisplay 验证 withdrawn（已撤回）动作的展示映射：
// 对账差集为被撤回告警落事件、以及未来 GitHub 若推送 withdrawn 动作时，
// 事件状态标签 / 通知状态 / 通用动作回退三处文案一致，不裸回退英文。
func TestAlertWithdrawnDisplay(t *testing.T) {
	ev := &store.Event{Kind: store.AlertKindDependabot, Action: store.AlertStateWithdrawn, Severity: "high", Title: "nanoid"}
	if got := EventStatusLabel(ev); got != "已撤回" {
		t.Fatalf("EventStatusLabel = %q, want 已撤回", got)
	}
	emoji, label := statusDisplay(ev)
	if label != "已撤回" {
		t.Fatalf("statusDisplay label = %q, want 已撤回", label)
	}
	if emoji != "🛡️" {
		t.Fatalf("statusDisplay emoji = %q, want 🛡️", emoji)
	}
	if got := actionDisplayName(store.AlertStateWithdrawn); got != "已撤回" {
		t.Fatalf("actionDisplayName = %q, want 已撤回", got)
	}
}
