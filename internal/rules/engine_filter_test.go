package rules

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// 仓库能力开关兜底：即使事件已入库，关闭对应开关的仓库不得外发实时通知。
func TestEngine按仓库能力开关兜底(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()

	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-all", ChannelType: store.ChannelTelegram, Name: "all", Enabled: true,
		Target: "1", DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-gate", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if err := data.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{ActionsEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	repo, err = data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}

	eng := &Engine{Store: data}
	runEvent := &store.Event{
		ID: ulid.Make().String(), Kind: "workflow_run", Action: "completed",
		Title: "ci", WorkflowConclusion: "failure", OccurredAt: time.Now().UTC(),
		RepositoryID: &repo.ID, DedupeFingerprint: ulid.Make().String(),
	}
	if err := eng.Evaluate(ctx, normalizer.Result{Event: runEvent, Repository: &repo}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	out, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("Actions 关闭的仓库不应外发 workflow_run，got %d 条", len(out))
	}

	// 监控总开关关闭：PR opened 这类本应实时的事件同样拦截。
	if err := data.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{MonitorEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	repo, err = data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	num := 5
	prEvent := &store.Event{
		ID: ulid.Make().String(), Kind: store.WorkItemKindPR, Action: "opened",
		Title: "pr", SubjectNumber: &num, OccurredAt: time.Now().UTC(),
		RepositoryID: &repo.ID, DedupeFingerprint: ulid.Make().String(),
	}
	if err := eng.Evaluate(ctx, normalizer.Result{Event: prEvent, Repository: &repo}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	out2, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out2) != 0 {
		t.Fatalf("监控关闭的仓库不应外发任何事件，got %d 条", len(out2))
	}
}

func TestEngine按渠道订阅过滤(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()

	// 渠道 A：只订阅 issue；渠道 B：nil=订阅全部。
	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-a", ChannelType: store.ChannelTelegram, Name: "a", Enabled: true,
		Target: "1", EventKinds: []string{store.WorkItemKindIssue}, DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: "ch-b", ChannelType: store.ChannelHTTPWebhook, Name: "b", Enabled: true,
		Target: "https://example.com", DigestEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	eng := &Engine{Store: data}
	num := 3
	prEvent := &store.Event{
		ID: ulid.Make().String(), Kind: store.WorkItemKindPR, Action: "opened",
		Title: "pr", SubjectNumber: &num, OccurredAt: time.Now().UTC(),
		DedupeFingerprint: ulid.Make().String(),
	}
	if err := eng.Evaluate(ctx, normalizer.Result{Event: prEvent}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	out, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ChannelID != "ch-b" {
		t.Fatalf("PR 事件应只投递给订阅全部的 ch-b，got %d 条: %+v", len(out), out)
	}

	issueEvent := &store.Event{
		ID: ulid.Make().String(), Kind: store.WorkItemKindIssue, Action: "opened",
		Title: "issue", SubjectNumber: &num, OccurredAt: time.Now().UTC(),
		DedupeFingerprint: ulid.Make().String(),
	}
	if err := eng.Evaluate(ctx, normalizer.Result{Event: issueEvent}, "acme/demo"); err != nil {
		t.Fatal(err)
	}
	out2, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out2) != 3 {
		t.Fatalf("issue 事件应双渠道投递，累计 3 条，got %d", len(out2))
	}
}
