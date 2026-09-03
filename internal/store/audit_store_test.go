package store_test

import (
	"context"
	"errors"
	"testing"

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
