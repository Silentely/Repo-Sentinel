package chatops

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestSecurity_ReadSecureConfig(t *testing.T) {
	st := newTestStore(t)
	ctx := t.Context()

	// 1. Not found -> returns empty string, no error
	val, err := ReadSecureConfig(ctx, st.Settings(), "non_existent_key")
	if err != nil || val != "" {
		t.Fatalf("expected empty string and nil error, got: %q, %v", val, err)
	}

	// 2. Normal quoted string
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "test.normal",
		ValueJSON: []byte(`"123456,789012"`),
		UpdatedAt: time.Now().UTC(),
	})
	val, err = ReadSecureConfig(ctx, st.Settings(), "test.normal")
	if err != nil || val != "123456,789012" {
		t.Fatalf("expected '123456,789012', got %q, %v", val, err)
	}

	// 3. Null value -> fail-closed error
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "test.null",
		ValueJSON: json.RawMessage(`null`),
		UpdatedAt: time.Now().UTC(),
	})
	_, err = ReadSecureConfig(ctx, st.Settings(), "test.null")
	if err == nil {
		t.Fatal("expected error on null config, got nil")
	}

	// 4. Blank string -> fail-closed error
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "test.blank",
		ValueJSON: []byte(`"   "`),
		UpdatedAt: time.Now().UTC(),
	})
	_, err = ReadSecureConfig(ctx, st.Settings(), "test.blank")
	if err == nil {
		t.Fatal("expected error on blank string config, got nil")
	}
}

func TestSecurity_VerifyOperatorWhitelist(t *testing.T) {
	st := newTestStore(t)
	ctx := t.Context()

	// 1. Empty/Missing whitelist -> allows everyone
	allowed, err := VerifyOperatorWhitelist(ctx, st.Settings(), "chatops.telegram.allowed_user_ids", "111")
	if err != nil || !allowed {
		t.Fatalf("expected allowed=true on empty whitelist, got: %v, %v", allowed, err)
	}

	// 2. Configured whitelist
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(`"111, 222, 333"`),
		UpdatedAt: time.Now().UTC(),
	})

	// Match
	allowed, err = VerifyOperatorWhitelist(ctx, st.Settings(), "chatops.telegram.allowed_user_ids", "222")
	if err != nil || !allowed {
		t.Fatalf("expected allowed=true for 222, got: %v, %v", allowed, err)
	}

	// No match
	allowed, err = VerifyOperatorWhitelist(ctx, st.Settings(), "chatops.telegram.allowed_user_ids", "444")
	if err != nil || allowed {
		t.Fatalf("expected allowed=false for 444, got: %v, %v", allowed, err)
	}
}

func TestSecurity_VerifyActorBinding(t *testing.T) {
	// 1. Unbound action -> allows any operator
	actionUnbound := &ActionData{ActorID: ""}
	if !VerifyActorBinding(actionUnbound, "any_user") {
		t.Fatal("expected unbound action to allow any operator")
	}

	// 2. Bound action -> only allows bound actor
	actionBound := &ActionData{ActorID: "bound_user_99"}
	if !VerifyActorBinding(actionBound, "bound_user_99") {
		t.Fatal("expected match for bound actor")
	}
	if VerifyActorBinding(actionBound, "other_user") {
		t.Fatal("expected mismatch for other actor")
	}
}
