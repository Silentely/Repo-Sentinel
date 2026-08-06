package store

import "testing"

// TestKindDisplayName 守护领域层事件类型中文名（通知正文与 AI 分诊共用）。
func TestKindDisplayName(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{WorkItemKindIssue, "Issue"},
		{WorkItemKindPR, "PR"},
		{AlertKindDependabot, "Dependabot 依赖告警"},
		{AlertKindCodeScanning, "Code Scanning 代码扫描"},
		{AlertKindSecretScanning, "Secret Scanning 密钥扫描"},
		{"workflow_run", "Actions 工作流"},
		{"custom_kind", "custom_kind"},
	}
	for _, tc := range cases {
		if got := KindDisplayName(tc.kind); got != tc.want {
			t.Errorf("KindDisplayName(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// TestPayloadString 守护 PayloadSummary 字符串读取：类型不符、nil、缺失键均返回空。
func TestPayloadString(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42, "slice": []any{"a"}}
	if got := PayloadString(m, "key"); got != "value" {
		t.Errorf("PayloadString 期望 value，实际 %q", got)
	}
	for _, key := range []string{"num", "slice", "missing"} {
		if got := PayloadString(m, key); got != "" {
			t.Errorf("PayloadString(%q) 应返回空，实际 %q", key, got)
		}
	}
	if got := PayloadString(nil, "key"); got != "" {
		t.Errorf("nil map 应返回空，实际 %q", got)
	}
}

// TestRepoAllowsKindStarWatch 守护 star/watch 新事件类型的仓库级能力门禁。
// 与 Issues/PR/Actions/Alerts 一致：字段默认值由 Ent schema Default(true) 保证，
// 结构体字面量不补默认值，故"默认放行"在此用显式 true 表达。
func TestRepoAllowsKindStarWatch(t *testing.T) {
	base := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, StarsEnabled: true, WatchesEnabled: true}
	if !RepoAllowsKind(&base, StarKind) {
		t.Fatal("star kind should be allowed when StarsEnabled")
	}
	if !RepoAllowsKind(&base, WatchKind) {
		t.Fatal("watch kind should be allowed when WatchesEnabled")
	}
	off := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, StarsEnabled: false}
	if RepoAllowsKind(&off, StarKind) {
		t.Fatal("star kind should be gated by StarsEnabled")
	}
	offWatch := Repository{MonitorEnabled: true, SyncStatus: SyncStatusActive, WatchesEnabled: false}
	if RepoAllowsKind(&offWatch, WatchKind) {
		t.Fatal("watch kind should be gated by WatchesEnabled")
	}
	archived := Repository{MonitorEnabled: true, SyncStatus: SyncStatusArchived}
	if RepoAllowsKind(&archived, StarKind) {
		t.Fatal("archived repo should not allow star kind")
	}
}

// TestIsSubscribableKindStarWatch 守护 star/watch 进入订阅白名单。
func TestIsSubscribableKindStarWatch(t *testing.T) {
	if !IsSubscribableKind(StarKind) || !IsSubscribableKind(WatchKind) {
		t.Fatal("star/watch should be subscribable")
	}
}
