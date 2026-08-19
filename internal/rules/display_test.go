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

// TestAlertUnknownActionDisplay 验证未收录告警动作统一回退「告警更新」：
// 不论是否携带严重度（此前的冗余分支），标签一致；emoji 随严重度变化。
func TestAlertUnknownActionDisplay(t *testing.T) {
	withSeverity := &store.Event{Kind: store.AlertKindDependabot, Action: "something_unknown", Severity: "high"}
	if got := EventStatusLabel(withSeverity); got != "告警更新" {
		t.Fatalf("EventStatusLabel(with severity) = %q, want 告警更新", got)
	}
	noSeverity := &store.Event{Kind: store.AlertKindCodeScanning, Action: "something_unknown"}
	if got := EventStatusLabel(noSeverity); got != "告警更新" {
		t.Fatalf("EventStatusLabel(no severity) = %q, want 告警更新", got)
	}
	emoji, label := statusDisplay(withSeverity)
	if label != "告警更新" || emoji != "🔴" {
		t.Fatalf("statusDisplay(with severity) = (%q, %q), want (🔴, 告警更新)", emoji, label)
	}
	emoji, label = statusDisplay(noSeverity)
	if label != "告警更新" || emoji != "🛡️" {
		t.Fatalf("statusDisplay(no severity) = (%q, %q), want (🛡️, 告警更新)", emoji, label)
	}
}

// TestKindEmoji 守护类别 emoji 单一来源：digest 报告分组行与通用回退共用，
// 扩展事件类别时只需改此处，避免与 digest 私有表漂移。
func TestKindEmoji(t *testing.T) {
	cases := map[string]string{
		store.WorkItemKindIssue:       "🐛",
		store.WorkItemKindPR:          "🔀",
		store.AlertKindDependabot:     "📦",
		store.AlertKindCodeScanning:   "🔎",
		store.AlertKindSecretScanning: "🔑",
		store.WorkflowRunKind:         "⚙️",
		store.StarKind:                "⭐",
		store.WatchKind:               "👀",
		store.ReleaseKind:             "🚀",
		"some_future_kind":            "📋",
	}
	for kind, want := range cases {
		if got := KindEmoji(kind); got != want {
			t.Errorf("KindEmoji(%q) = %q, want %q", kind, got, want)
		}
	}
}
