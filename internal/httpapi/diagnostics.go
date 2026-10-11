package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthStatus 状态枚举
type HealthStatus string

const (
	StatusHealthy   HealthStatus = "HEALTHY"
	StatusDegraded  HealthStatus = "DEGRADED"
	StatusUnhealthy HealthStatus = "UNHEALTHY"
)

// ComponentDiagnostic 单元组件诊断结果
type ComponentDiagnostic struct {
	Name    string       `json:"name"`
	Status  HealthStatus `json:"status"`
	Message string       `json:"message,omitempty"`
}

// SystemDiagnosticsReport 系统综合诊断报告
type SystemDiagnosticsReport struct {
	OverallStatus HealthStatus          `json:"overall_status"`
	Timestamp     time.Time             `json:"timestamp"`
	Components    []ComponentDiagnostic `json:"components"`
}

// EvaluateSystemDiagnostics 运行诊断检查并计算整体健康评级
func EvaluateSystemDiagnostics(components []ComponentDiagnostic) SystemDiagnosticsReport {
	overall := StatusHealthy
	for _, c := range components {
		if c.Status == StatusUnhealthy {
			overall = StatusUnhealthy
			break
		} else if c.Status == StatusDegraded && overall == StatusHealthy {
			overall = StatusDegraded
		}
	}

	return SystemDiagnosticsReport{
		OverallStatus: overall,
		Timestamp:     time.Now(),
		Components:    components,
	}
}

// DiagnosticsHandler 返回系统诊断报告 HTTP Handler
func DiagnosticsHandler(checker func() []ComponentDiagnostic) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var components []ComponentDiagnostic
		if checker != nil {
			components = checker()
		} else {
			components = []ComponentDiagnostic{
				{Name: "dispatcher", Status: StatusHealthy, Message: "Dispatcher ready"},
				{Name: "rules", Status: StatusHealthy, Message: "All rules active"},
			}
		}

		report := EvaluateSystemDiagnostics(components)
		w.Header().Set("Content-Type", "application/json")
		if report.OverallStatus == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(report)
	}
}
