package stats

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type mockStatsStore struct {
	store.Store
	workItems     []store.WorkItem
	workflowRuns  []store.WorkflowRun
	securityAlert []store.SecurityAlert
}

type mockWorkItemStore struct {
	store.WorkItemStore
	items []store.WorkItem
}

func (m *mockWorkItemStore) List(ctx context.Context, f store.ListFilter) ([]store.WorkItem, store.PageResult, error) {
	return m.items, store.PageResult{Total: len(m.items)}, nil
}

type mockWorkflowRunStore struct {
	store.WorkflowRunStore
	runs []store.WorkflowRun
}

func (m *mockWorkflowRunStore) List(ctx context.Context, f store.ListFilter) ([]store.WorkflowRun, store.PageResult, error) {
	return m.runs, store.PageResult{Total: len(m.runs)}, nil
}

type mockAlertStore struct {
	store.SecurityAlertStore
	alerts []store.SecurityAlert
}

func (m *mockAlertStore) List(ctx context.Context, f store.ListFilter) ([]store.SecurityAlert, store.PageResult, error) {
	return m.alerts, store.PageResult{Total: len(m.alerts)}, nil
}

func (m *mockStatsStore) WorkItems() store.WorkItemStore {
	return &mockWorkItemStore{items: m.workItems}
}

func (m *mockStatsStore) WorkflowRuns() store.WorkflowRunStore {
	return &mockWorkflowRunStore{runs: m.workflowRuns}
}

func (m *mockStatsStore) SecurityAlerts() store.SecurityAlertStore {
	return &mockAlertStore{alerts: m.securityAlert}
}

func TestCalculateHealthScorecard_Scenarios(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	// 1. 完美状态：A 级 (100分)
	succConclusion := "success"
	perfectStore := &mockStatsStore{
		workItems: []store.WorkItem{
			{Number: 1, Kind: store.WorkItemKindIssue, State: "open", UpdatedAt: now.Add(-1 * time.Hour)},
			{Number: 2, Kind: store.WorkItemKindPR, State: "open", UpdatedAt: now.Add(-2 * 24 * time.Hour)},
		},
		workflowRuns: []store.WorkflowRun{
			{Conclusion: &succConclusion},
			{Conclusion: &succConclusion},
		},
	}
	reportA, err := CalculateHealthScorecard(ctx, perfectStore, "repo-1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reportA.Score != 100 || reportA.Grade != "A" {
		t.Errorf("expected 100 A, got score=%d grade=%s", reportA.Score, reportA.Grade)
	}
	if len(reportA.StagnantItems) != 0 {
		t.Errorf("expected 0 stagnant items, got %d", len(reportA.StagnantItems))
	}

	// 2. 停滞严重 + 高危告警：D 级
	failConclusion := "failure"
	degradedStore := &mockStatsStore{
		workItems: []store.WorkItem{
			// 停滞 20 天的 Issue
			{Number: 10, Kind: store.WorkItemKindIssue, State: "open", UpdatedAt: now.Add(-20 * 24 * time.Hour)},
			// 停滞 40 天的 PR
			{Number: 11, Kind: store.WorkItemKindPR, State: "open", UpdatedAt: now.Add(-40 * 24 * time.Hour)},
		},
		workflowRuns: []store.WorkflowRun{
			{Conclusion: &failConclusion},
			{Conclusion: &failConclusion},
		},
		securityAlert: []store.SecurityAlert{
			{Severity: "critical", State: "open"},
			{Severity: "high", State: "open"},
		},
	}
	reportD, err := CalculateHealthScorecard(ctx, degradedStore, "repo-1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reportD.Grade != "D" || reportD.Score >= 60 {
		t.Errorf("expected grade D with score < 60, got score=%d grade=%s", reportD.Score, reportD.Grade)
	}
	if len(reportD.StagnantItems) != 2 {
		t.Errorf("expected 2 stagnant items, got %d", len(reportD.StagnantItems))
	}
	if !strings.Contains(reportD.Summary, "高危安全告警") {
		t.Errorf("expected summary to mention critical alert, got %s", reportD.Summary)
	}
}
