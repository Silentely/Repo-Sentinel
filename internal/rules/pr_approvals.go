package rules

// ReviewerState 单个评审人的审查状态
type ReviewerState struct {
	Username string `json:"username"`
	State    string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, PENDING
}

// PRApprovalSummary 审批决策概览
type PRApprovalSummary struct {
	TotalReviewers        int      `json:"total_reviewers"`
	ApprovedCount         int      `json:"approved_count"`
	ChangesRequestedCount int      `json:"changes_requested_count"`
	PendingCount          int      `json:"pending_count"`
	IsMergeReady          bool     `json:"is_merge_ready"`
	PendingUsers          []string `json:"pending_users"`
	StatusSummary         string   `json:"status_summary"`
}

// AggregatePRReviews 聚合评审矩阵并评估是否满足合并就绪条件
func AggregatePRReviews(reviewers []ReviewerState, minApprovalsRequired int) PRApprovalSummary {
	if minApprovalsRequired <= 0 {
		minApprovalsRequired = 1
	}

	summary := PRApprovalSummary{
		TotalReviewers: len(reviewers),
		PendingUsers:   make([]string, 0),
	}

	for _, r := range reviewers {
		switch r.State {
		case "APPROVED":
			summary.ApprovedCount++
		case "CHANGES_REQUESTED":
			summary.ChangesRequestedCount++
		default:
			summary.PendingCount++
			summary.PendingUsers = append(summary.PendingUsers, r.Username)
		}
	}

	// 合并就绪条件：必须没有任何 Changes Requested，且 Approved 数量达到要求
	if summary.ChangesRequestedCount == 0 && summary.ApprovedCount >= minApprovalsRequired {
		summary.IsMergeReady = true
		summary.StatusSummary = "🟢 审核通过，满足合并就绪条件"
	} else if summary.ChangesRequestedCount > 0 {
		summary.IsMergeReady = false
		summary.StatusSummary = "🔴 存在修改请求，阻塞合并"
	} else {
		summary.IsMergeReady = false
		summary.StatusSummary = "⏳ 仍在等待评审人员通过"
	}

	return summary
}
