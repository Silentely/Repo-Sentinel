package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestWebhookDeliveryStore_GetAndList(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)
	now := time.Now().UTC()

	d1, err := data.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 "del-1",
		DeliveryID:         "github-del-1",
		EventType:          "issues",
		Action:             "opened",
		RepositoryFullName: "org/repo-a",
		Status:             store.DeliveryProcessed,
		Payload:            []byte(`{"action":"opened"}`),
		ReceivedAt:         now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create delivery 1: %v", err)
	}

	d2, err := data.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 "del-2",
		DeliveryID:         "github-del-2",
		EventType:          "push",
		RepositoryFullName: "org/repo-b",
		Status:             store.DeliveryFailed,
		ErrorCode:          "syntax_error",
		Payload:            []byte(`{"ref":"main"}`),
		ReceivedAt:         now,
	})
	if err != nil {
		t.Fatalf("create delivery 2: %v", err)
	}

	// Test Get by ID
	got1, err := data.WebhookDeliveries().Get(ctx, d1.ID)
	if err != nil {
		t.Fatalf("get delivery 1: %v", err)
	}
	if got1.DeliveryID != "github-del-1" || string(got1.Payload) != `{"action":"opened"}` {
		t.Fatalf("unexpected delivery: %+v", got1)
	}

	// Test List all
	items, page, err := data.WebhookDeliveries().List(ctx, store.ListFilter{})
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if page.Total != 2 || len(items) != 2 {
		t.Fatalf("expected total 2, got total=%d, len=%d", page.Total, len(items))
	}
	// Verify descending order by ReceivedAt
	if items[0].ID != d2.ID {
		t.Fatalf("expected d2 first (latest), got %s", items[0].ID)
	}

	// Test List filtered by status
	failedItems, page, err := data.WebhookDeliveries().List(ctx, store.ListFilter{Status: store.DeliveryFailed})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if page.Total != 1 || len(failedItems) != 1 || failedItems[0].ID != "del-2" {
		t.Fatalf("unexpected filtered results: %+v", failedItems)
	}

	// Test List filtered by kind (event_type)
	issueItems, _, err := data.WebhookDeliveries().List(ctx, store.ListFilter{Kind: "issues"})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(issueItems) != 1 || issueItems[0].ID != "del-1" {
		t.Fatalf("unexpected issues results: %+v", issueItems)
	}

	// Test List filtered by repository
	repoBItems, _, err := data.WebhookDeliveries().List(ctx, store.ListFilter{RepositoryID: "repo-b"})
	if err != nil {
		t.Fatalf("list repo-b: %v", err)
	}
	if len(repoBItems) != 1 || repoBItems[0].ID != "del-2" {
		t.Fatalf("unexpected repo-b results: %+v", repoBItems)
	}
}
