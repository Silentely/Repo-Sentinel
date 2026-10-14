package rules

import (
	"context"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// ChatOpsActionData represents the payload for an interactive button action.
// 转发至 chatops.ActionData 保持完全向后兼容。
type ChatOpsActionData = chatops.ActionData

// TokenIssuer 声明规则引擎所需的令牌签发接口。
// 签名使用基础标量类型，绝不反向引用任何下游包，彻底杜绝循环依赖。
type TokenIssuer interface {
	CreateToken(ctx context.Context, action, repoID, runID, actorID string, ttl time.Duration) (string, error)
	CreateTokenWithPayload(ctx context.Context, action, repoID string, issueNumber int, payload, actorID string, ttl time.Duration) (string, error)
}

// CreateChatOpsToken stores a single-use action token with a specified TTL.
func CreateChatOpsToken(ctx context.Context, st store.Store, action, repoID, runID, actorID string, ttl time.Duration) (string, error) {
	return chatops.CreateChatOpsToken(ctx, st, action, repoID, runID, actorID, ttl)
}

// CreateChatOpsTokenWithPayload 创建包含扩展载荷（如 Issue 编号与回复草稿）的单次操作令牌。
func CreateChatOpsTokenWithPayload(ctx context.Context, st store.Store, action, repoID string, issueNumber int, payload, actorID string, ttl time.Duration) (string, error) {
	return chatops.CreateChatOpsTokenWithPayload(ctx, st, action, repoID, issueNumber, payload, actorID, ttl)
}

// CreateChatOpsTokenFull 存储包含完整配置的单次操作令牌。
func CreateChatOpsTokenFull(ctx context.Context, st store.Store, data ChatOpsActionData, ttl time.Duration) (string, error) {
	ts := chatops.NewTokenStore(st)
	return ts.Create(ctx, data, ttl)
}

// ConsumeChatOpsToken atomically retrieves and marks an action token as consumed.
func ConsumeChatOpsToken(ctx context.Context, st store.Store, tokenID string) (*ChatOpsActionData, error) {
	return chatops.ConsumeChatOpsToken(ctx, st, tokenID)
}

// ReleaseChatOpsToken 仅用于动作执行失败时释放已领取的 Token，允许安全重试。
func ReleaseChatOpsToken(ctx context.Context, st store.Store, tokenID string) error {
	return chatops.ReleaseChatOpsToken(ctx, st, tokenID)
}
