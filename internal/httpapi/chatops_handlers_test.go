package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestChatOps_Telegram_SecretTokenVerification(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Configure telegram secret token in settings
	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.telegram.secret_token",
		ValueJSON: json.RawMessage(`"my-secret-token-123"`),
	})
	if err != nil {
		t.Fatalf("failed to seed telegram secret token: %v", err)
	}

	payload := []byte(`{"update_id":123,"callback_query":{"id":"cb-1","data":"act:test-token"}}`)

	// Case 1: Missing header -> 403 Forbidden
	req := httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on missing secret token, got %d: %s", w.Code, w.Body.String())
	}

	// Case 2: Wrong header -> 403 Forbidden
	req = httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on wrong secret token, got %d: %s", w.Code, w.Body.String())
	}

	// Case 3: Correct header -> Accepted (200 OK)
	req = httptest.NewRequest(http.MethodPost, "/chatops/telegram/callback", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret-token-123")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on correct secret token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatOps_Feishu_URLVerificationAndSignature(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Configure feishu verification token in settings
	_, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		Key:       "chatops.feishu.verification_token",
		ValueJSON: json.RawMessage(`"feishu-token-xyz"`),
	})
	if err != nil {
		t.Fatalf("failed to seed feishu token: %v", err)
	}

	// Case 1: URL verification challenge
	challengePayload := []byte(`{"type":"url_verification","challenge":"challenge_code_999","token":"feishu-token-xyz"}`)
	req := httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(challengePayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on challenge, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["challenge"] != "challenge_code_999" {
		t.Fatalf("expected challenge 'challenge_code_999', got %v", res)
	}

	// Case 2: URL challenge with wrong token -> 401 Unauthorized
	wrongChallenge := []byte(`{"type":"url_verification","challenge":"abc","token":"wrong"}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(wrongChallenge))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong challenge token, got %d", w.Code)
	}

	// Case 3: Interactive card action with wrong token -> 401 Unauthorized
	wrongCard := []byte(`{"type":"interactive","token":"wrong","action":{"value":{"token":"tok123"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(wrongCard))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong interactive card token, got %d", w.Code)
	}

	// Case 4: Interactive card action with correct token -> 200 OK
	validCard := []byte(`{"type":"interactive","token":"feishu-token-xyz","action":{"value":{"token":"tok123"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/chatops/feishu/callback", bytes.NewReader(validCard))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid interactive card, got %d", w.Code)
	}
}

func TestChatOps_ActionToken_AntiReplay(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 1. Register a single-use action token
	tokenID, err := CreateChatOpsToken(t.Context(), fixture.store, "workflow_rerun", "repo-1", "123456", "user-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to create chatops token: %v", err)
	}

	// First consumption should succeed
	actionData, err := ConsumeChatOpsToken(t.Context(), fixture.store, tokenID)
	if err != nil {
		t.Fatalf("first token consumption should succeed, got: %v", err)
	}
	if actionData.Action != "workflow_rerun" || actionData.RepoID != "repo-1" {
		t.Fatalf("unexpected action data: %+v", actionData)
	}

	// Second consumption should fail immediately (consumed / replay rejected)
	_, err = ConsumeChatOpsToken(t.Context(), fixture.store, tokenID)
	if err == nil {
		t.Fatal("expected second consumption to fail with replay error, got nil")
	}
}
