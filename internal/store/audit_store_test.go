package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestAuditStoreBoundarySafety(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)

	// 1. 空 ID 检索直接返回 ErrNotFound
	if _, err := data.Audits().Get(ctx, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(\"\") = %v, want ErrNotFound", err)
	}
	if _, err := data.Audits().Get(ctx, "   "); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(\"   \") = %v, want ErrNotFound", err)
	}

	// 2. limit <= 0 返回空切片
	logs, err := data.Audits().List(ctx, 0, 0)
	if err != nil {
		t.Fatalf("List(0, 0): %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(logs))
	}

	// 3. 写入 zero time 时安全填充 UTC 时间
	created, err := data.Audits().Append(ctx, store.AuditLog{
		ID:         "audit-test-1",
		Action:     "test.action",
		ActorType:  "system",
		ActorID:    "sys",
		TargetType: "repository",
		TargetID:   "repo-1",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}

	// 4. 正确读出
	found, err := data.Audits().Get(ctx, "audit-test-1")
	if err != nil {
		t.Fatalf("Get(audit-test-1): %v", err)
	}
	if found.Action != "test.action" {
		t.Fatalf("found action = %q, want test.action", found.Action)
	}
}

func TestAuditStore_SanitizeMetadata(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)

	rawMap := map[string]any{
		"token":    "ghp_secrettoken1234567890",
		"password": "my_super_secret_password",
		"normal":   "safe_value",
		"nested": map[string]any{
			"api_key": "sk-1234567890abcdef",
			"info":    "hello",
		},
	}
	rawBytes, _ := json.Marshal(rawMap)

	created, err := data.Audits().Append(ctx, store.AuditLog{
		ID:           "audit-sanitize-1",
		Action:       "auth.login",
		ActorType:    "user",
		ActorID:      "admin",
		MetadataJSON: rawBytes,
	})
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	found, err := data.Audits().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var meta map[string]any
	if err := json.Unmarshal(found.MetadataJSON, &meta); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if meta["token"] == "ghp_secrettoken1234567890" {
		t.Fatalf("expected token to be sanitized, got %v", meta["token"])
	}
	if meta["password"] == "my_super_secret_password" {
		t.Fatalf("expected password to be sanitized, got %v", meta["password"])
	}
	if meta["normal"] != "safe_value" {
		t.Fatalf("expected normal to be preserved, got %v", meta["normal"])
	}
	nested, ok := meta["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", meta["nested"])
	}
	if nested["api_key"] == "sk-1234567890abcdef" {
		t.Fatalf("expected api_key to be sanitized, got %v", nested["api_key"])
	}
	if nested["info"] != "hello" {
		t.Fatalf("expected info to be preserved, got %v", nested["info"])
	}
}

func TestAuditStore_ListCursor(t *testing.T) {
	ctx := context.Background()
	data := openTestStore(t)

	baseTime := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		_, err := data.Audits().Append(ctx, store.AuditLog{
			ID:        fmt.Sprintf("audit-cur-%d", i),
			Action:    "action.test",
			ActorType: "user",
			ActorID:   "u1",
			CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("Append audit %d: %v", i, err)
		}
	}

	// Page 1: latest 2 items (should be audit-cur-5, audit-cur-4)
	page1, err := data.Audits().ListCursor(ctx, time.Time{}, "", 2)
	if err != nil {
		t.Fatalf("ListCursor page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].ID != "audit-cur-5" || page1[1].ID != "audit-cur-4" {
		t.Fatalf("unexpected page 1: %+v", page1)
	}

	// Page 2: next 2 items starting after page1 last item (audit-cur-4)
	page2, err := data.Audits().ListCursor(ctx, page1[1].CreatedAt, page1[1].ID, 2)
	if err != nil {
		t.Fatalf("ListCursor page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].ID != "audit-cur-3" || page2[1].ID != "audit-cur-2" {
		t.Fatalf("unexpected page 2: %+v", page2)
	}

	// Page 3: last 1 item
	page3, err := data.Audits().ListCursor(ctx, page2[1].CreatedAt, page2[1].ID, 2)
	if err != nil {
		t.Fatalf("ListCursor page 3: %v", err)
	}
	if len(page3) != 1 || page3[0].ID != "audit-cur-1" {
		t.Fatalf("unexpected page 3: %+v", page3)
	}
}
