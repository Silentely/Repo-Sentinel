package chatops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

var (
	ErrStoreUnavailable = errors.New("chatops: store unavailable")
	ErrTokenConsumed    = errors.New("token already consumed")
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenNotFound    = errors.New("chatops: token not found or invalid")
	ErrInvalidTokenData = errors.New("chatops: invalid token data")
)

// TokenStore 封装单次操作令牌的存储契约。
type TokenStore interface {
	Create(ctx context.Context, data ActionData, ttl time.Duration) (string, error)
	Consume(ctx context.Context, tokenID string) (*ActionData, error)
	Release(ctx context.Context, tokenID string) error
}

type defaultTokenStore struct {
	store store.Store
}

// NewTokenStore 基于系统底层 store 创建 TokenStore 实例。
func NewTokenStore(st store.Store) TokenStore {
	return &defaultTokenStore{store: st}
}

// Create 存储包含完整配置的单次操作令牌。
func (s *defaultTokenStore) Create(ctx context.Context, data ActionData, ttl time.Duration) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrStoreUnavailable
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
	_, err = s.store.Settings().Upsert(ctx, store.SystemSetting{
		ID:        id,
		Key:       store.ChatOpsTokenKey(id),
		ValueJSON: raw,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "chatops_issuer",
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// Consume 原子性获取并标记操作令牌为已消费。
func (s *defaultTokenStore) Consume(ctx context.Context, tokenID string) (*ActionData, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreUnavailable
	}
	key := store.ChatOpsTokenKey(tokenID)
	var data ActionData

	err := s.store.WithTx(ctx, func(tx store.Store) error {
		claimKey := store.ChatOpsClaimKey(tokenID)
		if _, err := tx.Settings().Create(ctx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       claimKey,
			ValueJSON: json.RawMessage(`{"claimed":true}`),
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "chatops_claim",
		}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrTokenConsumed
			}
			return err
		}
		setting, err := tx.Settings().Get(ctx, key)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTokenNotFound, err)
		}
		if err := json.Unmarshal(setting.ValueJSON, &data); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidTokenData, err)
		}
		if data.Consumed {
			return ErrTokenConsumed
		}
		if !data.ExpiresAt.IsZero() && time.Now().UTC().After(data.ExpiresAt) {
			return ErrTokenExpired
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
			UpdatedBy: "chatops_consumer",
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// Release 在动作执行安全失败时释放已领取的 Token，允许后续重试。
// 使用 context.WithoutCancel 派生带短超时的独立上下文，防止客户端连接提前断开导致回滚失败。
func (s *defaultTokenStore) Release(ctx context.Context, tokenID string) error {
	if s == nil || s.store == nil {
		return ErrStoreUnavailable
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	return s.store.WithTx(releaseCtx, func(tx store.Store) error {
		claimKey := store.ChatOpsClaimKey(tokenID)
		if err := tx.Settings().Delete(releaseCtx, claimKey); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		setting, err := tx.Settings().Get(releaseCtx, store.ChatOpsTokenKey(tokenID))
		if err != nil {
			return err
		}
		var data ActionData
		if err := json.Unmarshal(setting.ValueJSON, &data); err != nil {
			return err
		}
		data.Consumed = false
		data.ConsumedAt = time.Time{}
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = tx.Settings().Upsert(releaseCtx, store.SystemSetting{
			ID:        setting.ID,
			Key:       setting.Key,
			ValueJSON: raw,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "chatops_release",
		})
		return err
	})
}

// 包级便民函数，与存量函数签名对齐以平滑兼容。

func CreateChatOpsToken(ctx context.Context, st store.Store, action, repoID, runID, actorID string, ttl time.Duration) (string, error) {
	ts := NewTokenStore(st)
	return ts.Create(ctx, ActionData{
		Action:  action,
		RepoID:  repoID,
		RunID:   runID,
		ActorID: actorID,
	}, ttl)
}

func CreateChatOpsTokenWithPayload(ctx context.Context, st store.Store, action, repoID string, issueNumber int, payload, actorID string, ttl time.Duration) (string, error) {
	ts := NewTokenStore(st)
	return ts.Create(ctx, ActionData{
		Action:      action,
		RepoID:      repoID,
		IssueNumber: issueNumber,
		Payload:     payload,
		ActorID:     actorID,
	}, ttl)
}

func ConsumeChatOpsToken(ctx context.Context, st store.Store, tokenID string) (*ActionData, error) {
	ts := NewTokenStore(st)
	return ts.Consume(ctx, tokenID)
}

func ReleaseChatOpsToken(ctx context.Context, st store.Store, tokenID string) error {
	ts := NewTokenStore(st)
	return ts.Release(ctx, tokenID)
}
