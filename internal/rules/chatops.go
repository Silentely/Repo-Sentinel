package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ChatOpsActionData represents the payload for an interactive button action.
type ChatOpsActionData struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"` // e.g. "workflow_rerun", "issue_reply_draft", "issue_mark_spam_and_close"
	RepoID      string    `json:"repo_id"`
	RunID       string    `json:"run_id,omitempty"`
	IssueNumber int       `json:"issue_number,omitempty"`
	Payload     string    `json:"payload,omitempty"`
	ActorID     string    `json:"actor_id,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	Consumed    bool      `json:"consumed,omitempty"`
	ConsumedAt  time.Time `json:"consumed_at,omitempty"`
}

// CreateChatOpsToken stores a single-use action token with a specified TTL.
func CreateChatOpsToken(ctx context.Context, st store.Store, action, repoID, runID, actorID string, ttl time.Duration) (string, error) {
	return CreateChatOpsTokenFull(ctx, st, ChatOpsActionData{
		Action:  action,
		RepoID:  repoID,
		RunID:   runID,
		ActorID: actorID,
	}, ttl)
}

// CreateChatOpsTokenWithPayload 创建包含扩展载荷（如 Issue 编号与回复草稿）的单次操作令牌。
func CreateChatOpsTokenWithPayload(ctx context.Context, st store.Store, action, repoID string, issueNumber int, payload, actorID string, ttl time.Duration) (string, error) {
	return CreateChatOpsTokenFull(ctx, st, ChatOpsActionData{
		Action:      action,
		RepoID:      repoID,
		IssueNumber: issueNumber,
		Payload:     payload,
		ActorID:     actorID,
	}, ttl)
}

// CreateChatOpsTokenFull 存储包含完整配置的单次操作令牌。
func CreateChatOpsTokenFull(ctx context.Context, st store.Store, data ChatOpsActionData, ttl time.Duration) (string, error) {
	if st == nil {
		return "", errors.New("store not available")
	}
	expiresAt := time.Now().UTC().Add(ttl)
	if ttl == 0 {
		expiresAt = time.Now().UTC().Add(15 * time.Minute)
	}
	id := ulid.Make().String()
	data.ID = id
	data.ExpiresAt = expiresAt
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	_, err = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        id,
		Key:       store.ChatOpsTokenKey(id),
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
	key := store.ChatOpsTokenKey(tokenID)
	var data ChatOpsActionData

	err := st.WithTx(ctx, func(tx store.Store) error {
		if _, err := tx.Settings().Create(ctx, store.SystemSetting{
			ID: ulid.Make().String(), Key: store.ChatOpsClaimKey(tokenID),
			ValueJSON: json.RawMessage(`{"claimed":true}`), UpdatedAt: time.Now().UTC(),
		}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return errors.New("token already consumed")
			}
			return err
		}
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
			ID:        setting.ID,
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

// ReleaseChatOpsToken 仅用于动作执行失败时释放已领取的 Token，允许安全重试。
func ReleaseChatOpsToken(ctx context.Context, st store.Store, tokenID string) error {
	if st == nil {
		return errors.New("store not available")
	}
	return st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Settings().Delete(ctx, store.ChatOpsClaimKey(tokenID)); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		setting, err := tx.Settings().Get(ctx, store.ChatOpsTokenKey(tokenID))
		if err != nil {
			return err
		}
		var data ChatOpsActionData
		if err := json.Unmarshal(setting.ValueJSON, &data); err != nil {
			return err
		}
		data.Consumed = false
		data.ConsumedAt = time.Time{}
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = tx.Settings().Upsert(ctx, store.SystemSetting{Key: setting.Key, ValueJSON: raw, UpdatedAt: time.Now().UTC()})
		return err
	})
}
