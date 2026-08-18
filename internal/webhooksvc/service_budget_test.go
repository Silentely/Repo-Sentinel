package webhooksvc

import (
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
)

// TestProcessBudget 验证 webhook 处理预算跟随 AI 配置超时：
// 默认取系统下限 webhookProcessTimeout，AI 超时更高时放宽（预留非 AI 管线余量），
// 保证分诊调用预算不被处理预算截断。
func TestProcessBudget(t *testing.T) {
	cases := []struct {
		name string
		ai   *ai.Client
		want time.Duration
	}{
		{"nil 客户端用系统下限", nil, webhookProcessTimeout},
		{"未配置超时用系统下限", &ai.Client{}, webhookProcessTimeout},
		{"默认 30s 超时低于下限", &ai.Client{Timeout: 30 * time.Second}, webhookProcessTimeout},
		{"配置 60s 超时放宽", &ai.Client{Timeout: 60 * time.Second}, 70 * time.Second},
		{"配置 120s 超时放宽", &ai.Client{Timeout: 120 * time.Second}, 130 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := processBudget(tc.ai, webhookProcessTimeout); got != tc.want {
				t.Fatalf("processBudget = %s, want %s", got, tc.want)
			}
		})
	}
}
