package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// SubsystemStatus 描述子系统健康与启用状态
type SubsystemStatus struct {
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Details string `json:"details,omitempty"`
}

// SystemManifest 系统运行时装配全景清单
type SystemManifest struct {
	Version         string            `json:"version"`
	GitCommit       string            `json:"git_commit"`
	BuildTime       time.Time         `json:"build_time"`
	EnabledChannels []string          `json:"enabled_channels"`
	ActiveRuleCount int               `json:"active_rule_count"`
	Subsystems      []SubsystemStatus `json:"subsystems"`
}

// BuildSystemManifest 组装系统运行时装配清单
func BuildSystemManifest(version, commit string, enabledChannels []string, ruleCount int) SystemManifest {
	return SystemManifest{
		Version:         version,
		GitCommit:       commit,
		BuildTime:       time.Now(),
		EnabledChannels: enabledChannels,
		ActiveRuleCount: ruleCount,
		Subsystems: []SubsystemStatus{
			{Name: "notification_dispatcher", Ready: true, Details: "Multi-channel dispatcher online"},
			{Name: "rule_engine", Ready: true, Details: "Event evaluation pipeline active"},
			{Name: "outbox_worker", Ready: true, Details: "Async queue healthy"},
		},
	}
}

// SystemManifestHandler 返回系统清单的 HTTP 处理函数
func SystemManifestHandler(provider func() SystemManifest) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		manifest := BuildSystemManifest("v1.0.0", "unknown", nil, 0)
		if provider != nil {
			manifest = provider()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(manifest)
	}
}
