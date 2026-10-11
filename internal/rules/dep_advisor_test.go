package rules

import (
	"strings"
	"testing"
)

func TestRecommendDependencyMerge(t *testing.T) {
	// 1. 小版本补丁升级
	patchAdvice := RecommendDependencyMerge("update lodash", "1.2.1", "1.2.2")
	if !patchAdvice.SafeToMerge || !strings.Contains(patchAdvice.Recommendation, "风险较低") {
		t.Fatalf("patch 升级应推荐安全合并，got: %+v", patchAdvice)
	}

	// 2. 主版本大升级
	majorAdvice := RecommendDependencyMerge("update react", "v17.0.2", "v18.0.0")
	if majorAdvice.SafeToMerge || !strings.Contains(majorAdvice.Recommendation, "Major") {
		t.Fatalf("major 升级应标注破坏性风险，got: %+v", majorAdvice)
	}

	// 3. 标题包含 breaking
	breakingAdvice := RecommendDependencyMerge("feat!: breaking change update", "1.0.0", "1.1.0")
	if breakingAdvice.SafeToMerge {
		t.Fatalf("标题包含 breaking 应标注不建议自动合并")
	}
}
