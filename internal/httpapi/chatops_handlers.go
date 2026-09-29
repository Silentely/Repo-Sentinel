package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ChatOpsActionData represents the payload for an interactive button action.
type ChatOpsActionData struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"` // e.g. "workflow_rerun"
	RepoID     string    `json:"repo_id"`
	RunID      string    `json:"run_id"`
	ActorID    string    `json:"actor_id,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	Consumed   bool      `json:"consumed,omitempty"`
	ConsumedAt time.Time `json:"consumed_at,omitempty"`
}

// CreateChatOpsToken stores a single-use action token with a specified TTL.
func CreateChatOpsToken(ctx context.Context, st store.Store, action, repoID, runID, actorID string, ttl time.Duration) (string, error) {
	if st == nil {
		return "", errors.New("store not available")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	id := ulid.Make().String()
	data := ChatOpsActionData{
		ID:        id,
		Action:    action,
		RepoID:    repoID,
		RunID:     runID,
		ActorID:   actorID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	_, err = st.Settings().Upsert(ctx, store.SystemSetting{
		Key:       "chatops_token:" + id,
		ValueJSON: raw,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ConsumeChatOpsToken atomically retrieves and marks an action token as consumed.
func ConsumeChatOpsToken(ctx context.Context, st store.Store, tokenID string) (*ChatOpsActionData, error) {
	if st == nil {
		return nil, errors.New("store not available")
	}
	key := "chatops_token:" + tokenID
	var data ChatOpsActionData

	err := st.WithTx(ctx, func(tx store.Store) error {
		setting, err := tx.Settings().Get(ctx, key)
		if err != nil {
			return fmt.Errorf("token not found or invalid: %w", err)
		}
		if err := json.Unmarshal(setting.ValueJSON, &data); err != nil {
			return fmt.Errorf("invalid token data: %w", err)
		}
		if data.Consumed {
			return errors.New("token already consumed")
		}
		if !data.ExpiresAt.IsZero() && time.Now().UTC().After(data.ExpiresAt) {
			return errors.New("token expired")
		}

		data.Consumed = true
		data.ConsumedAt = time.Now().UTC()
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}

		_, err = tx.Settings().Upsert(ctx, store.SystemSetting{
			Key:       key,
			ValueJSON: raw,
			UpdatedAt: data.ConsumedAt,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// handleTelegramChatOpsCallback handles Telegram webhook callback queries.
func (s *server) handleTelegramChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	// 1. Verify Secret Token if configured
	if s.dependencies.Store != nil {
		setting, err := s.dependencies.Store.Settings().Get(r.Context(), "chatops.telegram.secret_token")
		if err == nil && len(setting.ValueJSON) > 0 {
			var expectedToken string
			if err := json.Unmarshal(setting.ValueJSON, &expectedToken); err == nil && expectedToken != "" {
				headerToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
				if subtle.ConstantTimeCompare([]byte(headerToken), []byte(expectedToken)) != 1 {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
					return
				}
			}
		}
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	var update struct {
		UpdateID      int `json:"update_id"`
		CallbackQuery *struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"from"`
		} `json:"callback_query"`
	}

	if err := json.Unmarshal(body, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	if update.CallbackQuery == nil || update.CallbackQuery.Data == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	data := update.CallbackQuery.Data
	if !strings.HasPrefix(data, "act:") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	tokenID := strings.TrimPrefix(data, "act:")
	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("telegram chatops token consumption failed", "token_id", tokenID, "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ignored", "reason": err.Error()})
		return
	}

	// Dispatch async execution
	s.safeGo("chatops_telegram", func() {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Info("telegram chatops action triggered",
				"action", action.Action,
				"repo_id", action.RepoID,
				"run_id", action.RunID,
				"actor", update.CallbackQuery.From.Username,
			)
		}
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFeishuChatOpsCallback handles Feishu interactive card events and URL verification.
func (s *server) handleFeishuChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	// 1. Verify Verification Token if configured (applies to challenge and interactive events)
	if s.dependencies.Store != nil {
		setting, err := s.dependencies.Store.Settings().Get(r.Context(), "chatops.feishu.verification_token")
		if err == nil && len(setting.ValueJSON) > 0 {
			var expectedToken string
			if err := json.Unmarshal(setting.ValueJSON, &expectedToken); err == nil && expectedToken != "" {
				token, _ := body["token"].(string)
				if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
			}
		}
	}

	// 2. Handle URL Verification Challenge
	if msgType, ok := body["type"].(string); ok && msgType == "url_verification" {
		challenge, _ := body["challenge"].(string)
		writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}

	// 2. Handle Interactive Card Action
	// In Feishu, action value can be in body["action"].(map[string]any)["value"]
	var tokenID string
	if actionObj, ok := body["action"].(map[string]any); ok {
		if valObj, ok := actionObj["value"].(map[string]any); ok {
			if tok, ok := valObj["token"].(string); ok {
				tokenID = tok
			}
		} else if valStr, ok := actionObj["value"].(string); ok {
			tokenID = strings.TrimPrefix(valStr, "act:")
		}
	}

	if tokenID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "info",
				"content": "No action specified",
			},
		})
		return
	}

	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("feishu chatops token consumption failed", "token_id", tokenID, "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "warning",
				"content": "Action expired or already performed",
			},
		})
		return
	}

	// Dispatch async execution
	s.safeGo("chatops_feishu", func() {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Info("feishu chatops action triggered",
				"action", action.Action,
				"repo_id", action.RepoID,
				"run_id", action.RunID,
			)
		}
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"toast": map[string]string{
			"type":    "success",
			"content": "Action queued successfully",
		},
	})
}
