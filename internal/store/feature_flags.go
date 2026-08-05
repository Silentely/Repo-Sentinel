package store

import (
	"context"
	"encoding/json"
)

// 全局功能模块开关在 system_settings 中的键名。
// 关闭后：停止对应类型的采集、对账与实时/摘要通知；默认 true（与管理台默认一致）。
const (
	SettingFeatureIssues         = "feature.issues"
	SettingFeaturePullRequests   = "feature.pull_requests"
	SettingFeatureActions        = "feature.actions"
	SettingFeatureSecurityAlerts = "feature.security_alerts"
)

// FeatureEnabled 读取布尔型全局功能开关。
// 键不存在、JSON 非法或非 bool 时返回 true，避免未配置时误伤采集。
func FeatureEnabled(ctx context.Context, settings SettingsStore, key string) bool {
	if settings == nil {
		return true
	}
	row, err := settings.Get(ctx, key)
	if err != nil {
		return true
	}
	var v bool
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return true
	}
	return v
}

// KindFeatureKey 将事件/资源 kind 映射到全局功能设置键。
// 无法映射时返回空字符串（调用方应视为放行）。
func KindFeatureKey(kind string) string {
	switch kind {
	case WorkItemKindIssue:
		return SettingFeatureIssues
	case WorkItemKindPR:
		return SettingFeaturePullRequests
	case WorkflowRunKind:
		return SettingFeatureActions
	case AlertKindDependabot, AlertKindCodeScanning, AlertKindSecretScanning:
		return SettingFeatureSecurityAlerts
	default:
		return ""
	}
}

// KindFeatureEnabled 判定 kind 对应的全局功能是否开启；未知 kind 默认 true。
func KindFeatureEnabled(ctx context.Context, settings SettingsStore, kind string) bool {
	key := KindFeatureKey(kind)
	if key == "" {
		return true
	}
	return FeatureEnabled(ctx, settings, key)
}

// FeatureFlags 一次读取四个全局功能开关，供摘要等批量过滤使用。
type FeatureFlags struct {
	Issues         bool
	PullRequests   bool
	Actions        bool
	SecurityAlerts bool
}

// LoadFeatureFlags 加载全部功能开关；缺省均为 true。
func LoadFeatureFlags(ctx context.Context, settings SettingsStore) FeatureFlags {
	return FeatureFlags{
		Issues:         FeatureEnabled(ctx, settings, SettingFeatureIssues),
		PullRequests:   FeatureEnabled(ctx, settings, SettingFeaturePullRequests),
		Actions:        FeatureEnabled(ctx, settings, SettingFeatureActions),
		SecurityAlerts: FeatureEnabled(ctx, settings, SettingFeatureSecurityAlerts),
	}
}

// AllowsKind 判定当前功能标志是否放行该 kind。
func (f FeatureFlags) AllowsKind(kind string) bool {
	switch kind {
	case WorkItemKindIssue:
		return f.Issues
	case WorkItemKindPR:
		return f.PullRequests
	case WorkflowRunKind:
		return f.Actions
	case AlertKindDependabot, AlertKindCodeScanning, AlertKindSecretScanning:
		return f.SecurityAlerts
	default:
		return true
	}
}
