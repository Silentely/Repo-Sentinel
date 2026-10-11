package rules

import (
	"strings"
)

// DependencyMergeAdvice 依赖更新合并建议。
type DependencyMergeAdvice struct {
	SafeToMerge    bool   `json:"safe_to_merge"`
	Recommendation string `json:"recommendation"`
}

// RecommendDependencyMerge 依据版本号迁移跨度研判依赖 PR 的合并安全度。
func RecommendDependencyMerge(title, fromVer, toVer string) DependencyMergeAdvice {
	cleanFrom := strings.TrimPrefix(strings.TrimSpace(fromVer), "v")
	cleanTo := strings.TrimPrefix(strings.TrimSpace(toVer), "v")

	partsFrom := strings.Split(cleanFrom, ".")
	partsTo := strings.Split(cleanTo, ".")

	if len(partsFrom) > 0 && len(partsTo) > 0 && partsFrom[0] != "" && partsTo[0] != "" {
		if partsFrom[0] != partsTo[0] {
			return DependencyMergeAdvice{
				SafeToMerge:    false,
				Recommendation: "⚠️ 跨主版本大升级 (Major)，可能包含破坏性变更，建议人工复核测试",
			}
		}
	}

	lower := strings.ToLower(title)
	if strings.Contains(lower, "major") || strings.Contains(lower, "breaking") {
		return DependencyMergeAdvice{
			SafeToMerge:    false,
			Recommendation: "⚠️ 标题标注破坏性变更，需谨慎合并",
		}
	}

	return DependencyMergeAdvice{
		SafeToMerge:    true,
		Recommendation: "🟢 补丁/小版本平滑升级 (Minor/Patch)，风险较低",
	}
}
