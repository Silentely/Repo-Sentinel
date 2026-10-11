package rules

// PRMergeableInspection 描述 PR 可合入性预检评估结果
type PRMergeableInspection struct {
	CanAutoMerge     bool   `json:"can_auto_merge"`
	ConflictDetected bool   `json:"conflict_detected"`
	StatusBadge      string `json:"status_badge"`
	Recommendation   string `json:"recommendation"`
}

// InspectPRMergeability 评估 PR 是否存在分支冲突或是否满足自动合并要求
func InspectPRMergeability(mergeable *bool, mergeableState string, isDraft bool) PRMergeableInspection {
	if isDraft {
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: false,
			StatusBadge:      "📝 草稿状态",
			Recommendation:   "PR 处于 Draft 阶段，需发布为就绪后再审查合入",
		}
	}

	if mergeable != nil && !*mergeable {
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: true,
			StatusBadge:      "⚔️ 存在代码冲突",
			Recommendation:   "目标分支代码已被更新，需拉取最新代码解决冲突后重推",
		}
	}

	switch mergeableState {
	case "clean":
		return PRMergeableInspection{
			CanAutoMerge:     true,
			ConflictDetected: false,
			StatusBadge:      "🟢 绿灯就绪",
			Recommendation:   "代码无冲突且所有检查均通过，可直接安全合并",
		}
	case "behind":
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: false,
			StatusBadge:      "🟡 需同步基准分支",
			Recommendation:   "分支落后于目标分支，建议点击 Update Branch 同步基准代码",
		}
	case "blocked":
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: false,
			StatusBadge:      "🛑 合并受阻",
			Recommendation:   "CI 检查未完全通过或缺少必须的代码评审批准",
		}
	case "dirty":
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: true,
			StatusBadge:      "⚔️ 存在代码冲突",
			Recommendation:   "分支存在文件冲突，请解决冲突后重新触发 CI",
		}
	default:
		return PRMergeableInspection{
			CanAutoMerge:     false,
			ConflictDetected: false,
			StatusBadge:      "⏳ 检查进行中",
			Recommendation:   "GitHub 正在计算分支差异与状态检查",
		}
	}
}
