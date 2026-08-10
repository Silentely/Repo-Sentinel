package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func putFeature(t *testing.T, data store.Store, key string, enabled bool) {
	t.Helper()
	raw, _ := json.Marshal(enabled)
	_, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: key, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFeatureEnabled_默认与显式开关(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()

	// 未写入时默认开启。
	if !store.FeatureEnabled(ctx, data.Settings(), store.SettingFeatureActions) {
		t.Fatal("未配置时 feature.actions 应默认 true")
	}

	putFeature(t, data, store.SettingFeatureActions, false)
	if store.FeatureEnabled(ctx, data.Settings(), store.SettingFeatureActions) {
		t.Fatal("显式 false 后 feature.actions 应为 false")
	}

	putFeature(t, data, store.SettingFeatureActions, true)
	if !store.FeatureEnabled(ctx, data.Settings(), store.SettingFeatureActions) {
		t.Fatal("显式 true 后 feature.actions 应为 true")
	}
}

func TestKindFeatureEnabled_按类型映射(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()

	putFeature(t, data, store.SettingFeatureActions, false)
	putFeature(t, data, store.SettingFeatureIssues, false)

	if store.KindFeatureEnabled(ctx, data.Settings(), "workflow_run") {
		t.Fatal("workflow_run 应受 feature.actions 关闭约束")
	}
	if store.KindFeatureEnabled(ctx, data.Settings(), store.WorkItemKindIssue) {
		t.Fatal("issue 应受 feature.issues 关闭约束")
	}
	// PR 未写设置，默认 true。
	if !store.KindFeatureEnabled(ctx, data.Settings(), store.WorkItemKindPR) {
		t.Fatal("未配置的 pull_request 应默认 true")
	}
	// 未知 kind 放行。
	if !store.KindFeatureEnabled(ctx, data.Settings(), "installation") {
		t.Fatal("未知 kind 应默认放行")
	}
}

func TestLoadFeatureFlags_AllowsKind(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	putFeature(t, data, store.SettingFeatureSecurityAlerts, false)

	flags := store.LoadFeatureFlags(ctx, data.Settings())
	if !flags.Issues || !flags.PullRequests || !flags.Actions {
		t.Fatalf("未关闭的模块应仍为 true: %+v", flags)
	}
	if flags.SecurityAlerts {
		t.Fatal("安全告警全局关闭后应为 false")
	}
	if flags.AllowsKind(store.AlertKindDependabot) {
		t.Fatal("dependabot 应被全局安全告警开关拦截")
	}
	if !flags.AllowsKind("workflow_run") {
		t.Fatal("actions 未关时 workflow_run 应放行")
	}
}

// TestKindFeatureKeyStarWatch 守护 star/watch 到全局功能开关键的映射。
func TestKindFeatureKeyStarWatch(t *testing.T) {
	if got := store.KindFeatureKey(store.StarKind); got != store.SettingFeatureStars {
		t.Fatalf("star kind key = %q, want %q", got, store.SettingFeatureStars)
	}
	if got := store.KindFeatureKey(store.WatchKind); got != store.SettingFeatureWatches {
		t.Fatalf("watch kind key = %q, want %q", got, store.SettingFeatureWatches)
	}
}

// TestReleaseKindFeature 守护 release 事件类型到全局功能开关的映射与订阅白名单。
func TestReleaseKindFeature(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()

	if !store.IsSubscribableKind(store.ReleaseKind) {
		t.Fatal("release 应在渠道订阅白名单内")
	}
	if got := store.KindFeatureKey(store.ReleaseKind); got != store.SettingFeatureStarredReleases {
		t.Fatalf("KindFeatureKey(release) = %q, want %q", got, store.SettingFeatureStarredReleases)
	}
	if !store.KindFeatureEnabled(ctx, data.Settings(), store.ReleaseKind) {
		t.Fatal("键缺失默认应开启")
	}
	putFeature(t, data, store.SettingFeatureStarredReleases, false)
	if store.KindFeatureEnabled(ctx, data.Settings(), store.ReleaseKind) {
		t.Fatal("关闭后应生效")
	}
	flags := store.LoadFeatureFlags(ctx, data.Settings())
	if flags.StarredReleases {
		t.Fatal("LoadFeatureFlags 应反映关闭状态")
	}
	if flags.AllowsKind(store.ReleaseKind) {
		t.Fatal("AllowsKind 应拦截关闭的 release")
	}
}
