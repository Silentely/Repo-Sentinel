package rules

import (
	"strings"
	"testing"
)

func TestAggregatePRReviews_MergeReady(t *testing.T) {
	reviewers := []ReviewerState{
		{Username: "alice", State: "APPROVED"},
		{Username: "bob", State: "APPROVED"},
	}

	summary := AggregatePRReviews(reviewers, 2)
	if !summary.IsMergeReady {
		t.Errorf("expected merge ready, got false")
	}
	if summary.ApprovedCount != 2 || len(summary.PendingUsers) != 0 {
		t.Errorf("unexpected summary counts: %+v", summary)
	}
	if !strings.Contains(summary.StatusSummary, "🟢") {
		t.Errorf("unexpected status summary: %s", summary.StatusSummary)
	}
}

func TestAggregatePRReviews_ChangesRequested(t *testing.T) {
	reviewers := []ReviewerState{
		{Username: "alice", State: "APPROVED"},
		{Username: "charlie", State: "CHANGES_REQUESTED"},
		{Username: "david", State: "PENDING"},
	}

	summary := AggregatePRReviews(reviewers, 1)
	if summary.IsMergeReady {
		t.Errorf("expected merge blocked by changes requested")
	}
	if summary.ChangesRequestedCount != 1 || summary.PendingCount != 1 {
		t.Errorf("unexpected counts: %+v", summary)
	}
	if len(summary.PendingUsers) != 1 || summary.PendingUsers[0] != "david" {
		t.Errorf("expected david in pending users: %v", summary.PendingUsers)
	}
}
