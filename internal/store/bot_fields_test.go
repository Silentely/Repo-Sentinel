package store_test

import (
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestBotFieldsPersistenceRoundTrip(t *testing.T) {
	ctx := t.Context()
	data := openTestStore(t)

	// 1. NotificationChannel.IgnoreBots
	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          ulid.Make().String(),
		ChannelType: store.ChannelTelegram,
		Name:        "tg-bot-test",
		Enabled:     true,
		Target:      "12345",
		IgnoreBots:  true,
	})
	if err != nil {
		t.Fatalf("channel upsert failed: %v", err)
	}
	gotCh, err := data.Channels().Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("channel get failed: %v", err)
	}
	if !gotCh.IgnoreBots {
		t.Errorf("expected IgnoreBots=true, got false")
	}

	// 更新渠道关闭 IgnoreBots
	gotCh.IgnoreBots = false
	if _, err := data.Channels().Upsert(ctx, gotCh); err != nil {
		t.Fatalf("channel update failed: %v", err)
	}
	gotChUpdated, err := data.Channels().Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("channel get updated failed: %v", err)
	}
	if gotChUpdated.IgnoreBots {
		t.Errorf("expected IgnoreBots=false, got true")
	}

	// 2. Repository setup for WorkItem and Event
	repo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID:       ulid.Make().String(),
		Type:     store.RepositoryTypeInstallation,
		Owner:    "test-owner",
		Name:     "test-repo",
		FullName: "test-owner/test-repo",
	})
	if err != nil {
		t.Fatalf("repo upsert failed: %v", err)
	}

	// 3. WorkItem.AuthorIsBot
	wi, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              ulid.Make().String(),
		RepositoryID:    repo.ID,
		Number:          101,
		Kind:            store.WorkItemKindPR,
		State:           "open",
		Title:           "Dependabot bump",
		Author:          "dependabot[bot]",
		AuthorIsBot:     true,
		SourceUpdatedAt: time.Now().UTC(),
		StateHash:       "hash-1",
	}, nil)
	if err != nil {
		t.Fatalf("work item upsert failed: %v", err)
	}
	_ = wi
	gotWI, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 101)
	if err != nil {
		t.Fatalf("work item get failed: %v", err)
	}
	if !gotWI.AuthorIsBot {
		t.Errorf("expected WorkItem.AuthorIsBot=true, got false")
	}

	// 4. Event.SenderIsBot
	ev, err := data.Events().Create(ctx, store.Event{
		ID:                ulid.Make().String(),
		Source:            "webhook",
		Kind:              store.WorkItemKindPR,
		Action:            "opened",
		RepositoryID:      &repo.ID,
		Actor:             "dependabot[bot]",
		SenderIsBot:       true,
		OccurredAt:        time.Now().UTC(),
		DedupeFingerprint: "fp-bot-event-1",
	})
	if err != nil {
		t.Fatalf("event create failed: %v", err)
	}
	_ = ev
	gotEv, err := data.Events().GetByFingerprint(ctx, "fp-bot-event-1")
	if err != nil {
		t.Fatalf("event get failed: %v", err)
	}
	if !gotEv.SenderIsBot {
		t.Errorf("expected Event.SenderIsBot=true, got false")
	}
	if gotEv.Actor != "dependabot[bot]" {
		t.Errorf("expected Actor to remain dependabot[bot], got %s", gotEv.Actor)
	}
}
