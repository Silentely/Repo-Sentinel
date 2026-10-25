package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrBudgetExceeded 当月已消耗 Token 达到或超过配置的月度上限时拒绝继续发起请求。
	ErrBudgetExceeded = errors.New("ai: monthly token budget exceeded")
	monthlyUsageMu    sync.Mutex
)

func currentMonthKey() string {
	return "ai.usage." + time.Now().UTC().Format("2006-01")
}

// CheckMonthlyBudget 检查当前月份是否已超出 Token 预算上限。limitTokens <= 0 时不限制。
func CheckMonthlyBudget(ctx context.Context, s store.SettingsStore, limitTokens int64) error {
	if s == nil || limitTokens <= 0 {
		return nil
	}
	setting, err := s.Get(ctx, currentMonthKey())
	if err != nil {
		return nil
	}
	var used int64
	_ = json.Unmarshal(setting.ValueJSON, &used)
	if used >= limitTokens {
		return ErrBudgetExceeded
	}
	return nil
}

// RecordMonthlyUsage 将实际消耗的 Prompt 与 Completion Tokens 累加至持久化月度记录中。
func RecordMonthlyUsage(ctx context.Context, s store.SettingsStore, promptTokens, completionTokens int) {
	if s == nil {
		return
	}
	total := int64(promptTokens + completionTokens)
	if total <= 0 {
		return
	}
	monthlyUsageMu.Lock()
	defer monthlyUsageMu.Unlock()

	key := currentMonthKey()
	setting, err := s.Get(ctx, key)
	var prev int64
	if err == nil {
		_ = json.Unmarshal(setting.ValueJSON, &prev)
	}
	raw, _ := json.Marshal(prev + total)
	now := time.Now().UTC()
	_, _ = s.Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       key,
		ValueJSON: raw,
		UpdatedAt: now,
		UpdatedBy: "ai_telemetry",
	})
}

// GetMonthlyUsage 读取当月累计消耗的 Token 数量。
func GetMonthlyUsage(ctx context.Context, s store.SettingsStore) int64 {
	if s == nil {
		return 0
	}
	setting, err := s.Get(ctx, currentMonthKey())
	if err != nil {
		return 0
	}
	var used int64
	_ = json.Unmarshal(setting.ValueJSON, &used)
	return used
}

// 进程内 AI 调用指标（单实例 Prometheus 文本暴露；多副本各自独立）。
// 计数在 Complete 出口统一进行，与日志留痕同源，保证成功/失败口径一致；
// 未配置（ErrNotConfigured）不发起请求，不计入任何指标。
var (
	aiRequests         atomic.Uint64 // 实际发起的调用次数（成功+失败）
	aiFailures         atomic.Uint64 // 失败调用次数
	aiDurationMS       atomic.Uint64 // 调用耗时累计（毫秒，成功+失败）
	aiPromptTokens     atomic.Uint64 // 上游返回的输入 token 累计
	aiCompletionTokens atomic.Uint64 // 上游返回的输出 token 累计
	aiFailByCodeMu     sync.Mutex
	aiFailByCode       = map[string]uint64{} // 失败次数按 error_code 分列
)

// metricsIncResult 在调用出口累计一次结果。failed 为 true 时按分类累加失败次数。
func metricsIncResult(failed bool, code string, d time.Duration, prompt, completion int) {
	aiRequests.Add(1)
	aiDurationMS.Add(uint64(d.Milliseconds()))
	if failed {
		aiFailures.Add(1)
		aiFailByCodeMu.Lock()
		aiFailByCode[code]++
		aiFailByCodeMu.Unlock()
		return
	}
	if prompt > 0 {
		aiPromptTokens.Add(uint64(prompt))
	}
	if completion > 0 {
		aiCompletionTokens.Add(uint64(completion))
	}
}

// MetricsSnapshot 返回当前 AI 指标快照，供 /metrics 暴露。
// failByCode 为按 error_code 排序的只读副本。
func MetricsSnapshot() (requests, failures, durationMS, promptTokens, completionTokens uint64, failByCode map[string]uint64) {
	aiFailByCodeMu.Lock()
	byCode := make(map[string]uint64, len(aiFailByCode))
	for code, n := range aiFailByCode {
		byCode[code] = n
	}
	aiFailByCodeMu.Unlock()
	return aiRequests.Load(), aiFailures.Load(), aiDurationMS.Load(),
		aiPromptTokens.Load(), aiCompletionTokens.Load(), byCode
}

// SortedFailCodes 返回 failByCode 的键排序，保证 /metrics 输出稳定（可测试）。
func SortedFailCodes(byCode map[string]uint64) []string {
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
