package chatops

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/sideeffect"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

var (
	ErrExecutorUnavailable  = errors.New("chatops: executor unavailable")
	ErrSecurityConfigBroken = ErrSecurityCorrupted
	ErrInvalidToken         = ErrTokenNotFound
)

// TelegramCallbackRequest 封装 Telegram 回调已解析的领域数据。
type TelegramCallbackRequest struct {
	CallbackQueryID string
	Data            string
	FromID          int64
	FromUsername    string
	MessageID       int
	ChatID          int64
	InlineKeyboard  [][]map[string]any
}

// FeishuCallbackRequest 封装飞书卡片互动已解析的领域数据。
type FeishuCallbackRequest struct {
	TokenID        string
	OperatorOpenID string
}

// SlackCallbackRequest 封装 Slack 互动组件已解析的领域数据。
type SlackCallbackRequest struct {
	TokenID     string
	UserID      string
	ResponseURL string
}

// Service 声明 ChatOps 领域子系统门面接口。
type Service interface {
	HandleTelegram(ctx context.Context, in TelegramCallbackRequest) (*Outcome, error)
	HandleFeishu(ctx context.Context, in FeishuCallbackRequest) (*Outcome, error)
	HandleSlack(ctx context.Context, in SlackCallbackRequest) (*Outcome, error)
	CreateToken(ctx context.Context, data ActionData, ttl time.Duration) (string, error)
}

// Dependencies 注入 ChatOps 服务所需外部依赖。
type Dependencies struct {
	Store            store.Store
	TokenStore       TokenStore
	Executor         ActionExecutor
	TelegramFeedback TelegramFeedback
	Logger           *slog.Logger
	SafeGo           func(name string, fn func())
}

type service struct {
	store    store.Store
	tokens   TokenStore
	executor ActionExecutor
	feedback TelegramFeedback
	logger   *slog.Logger
	safeGo   func(name string, fn func())
}

// NewService 实例化 ChatOps 领域服务。
func NewService(deps Dependencies) Service {
	tokens := deps.TokenStore
	if tokens == nil && deps.Store != nil {
		tokens = NewTokenStore(deps.Store)
	}
	fb := deps.TelegramFeedback
	if fb == nil && deps.Store != nil {
		fb = NewDefaultTelegramFeedback(deps.Store, nil)
	}
	sg := deps.SafeGo
	if sg == nil {
		sg = func(_ string, fn func()) {
			go fn()
		}
	}
	return &service{
		store:    deps.Store,
		tokens:   tokens,
		executor: deps.Executor,
		feedback: fb,
		logger:   deps.Logger,
		safeGo:   sg,
	}
}

func (s *service) CreateToken(ctx context.Context, data ActionData, ttl time.Duration) (string, error) {
	if s.tokens == nil {
		return "", ErrStoreUnavailable
	}
	return s.tokens.Create(ctx, data, ttl)
}

func (s *service) dispatchExecutor(ctx context.Context, action ActionData) (*Outcome, error) {
	if s.executor == nil {
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "执行器暂不可用",
			Err:     ErrExecutorUnavailable,
		}, ErrExecutorUnavailable
	}
	if outExec, ok := s.executor.(interface {
		ExecuteWithOutcome(context.Context, ActionData) (*Outcome, error)
	}); ok {
		return outExec.ExecuteWithOutcome(ctx, action)
	}

	// 兼容旧 ActionExecutor 分支的保守安全错误分类（P0 / HIGH 4）
	execErr := s.executor.Execute(ctx, action)
	if execErr == nil {
		return &Outcome{
			Status:  StatusExecuted,
			Action:  action.Action,
			Message: "操作已成功完成",
		}, nil
	}
	if sideeffect.IsNetworkUnknownError(execErr) ||
		strings.Contains(strings.ToLower(execErr.Error()), "unknown-receipt persist failed") ||
		strings.Contains(strings.ToLower(execErr.Error()), "network unknown") ||
		errors.Is(execErr, context.DeadlineExceeded) ||
		errors.Is(execErr, context.Canceled) {
		return &Outcome{
			Status:  StatusIndeterminate,
			Action:  action.Action,
			Message: "操作执行超时或网络在途不确定，变更结果待确认",
			Err:     execErr,
		}, execErr
	}
	if sideeffect.IsPreSendError(execErr) {
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "参数校验失败，允许修正后重试",
			Err:     execErr,
		}, execErr
	}
	if sideeffect.IsRemoteSucceededReceiptFailed(execErr) ||
		strings.Contains(strings.ToLower(execErr.Error()), "succeeded externally but receipt persist failed") {
		return &Outcome{
			Status:  StatusIndeterminate,
			Action:  action.Action,
			Message: "操作在远端已生效，但本地回执落库异常",
			Err:     execErr,
		}, execErr
	}
	return &Outcome{
		Status:  StatusRetryableFailure,
		Action:  action.Action,
		Message: "操作执行失败",
		Err:     execErr,
	}, execErr
}

func (s *service) HandleTelegram(ctx context.Context, in TelegramCallbackRequest) (*Outcome, error) {
	data := strings.TrimSpace(in.Data)
	if data == "" {
		return &Outcome{Status: StatusAlreadyDone, Action: "empty", Message: "ok"}, nil
	}

	// 1. 幂等完成态检查
	if strings.HasPrefix(data, "done:") {
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_done", func() {
				s.feedback.AnswerActionCallback(context.Background(), in.CallbackQueryID, data)
			})
		}
		return &Outcome{
			Status:  StatusAlreadyDone,
			Action:  data,
			Message: "操作已完成",
		}, nil
	}

	if !strings.HasPrefix(data, "act:") && !strings.HasPrefix(data, "rerun:") {
		return &Outcome{
			Status:  StatusAlreadyDone,
			Action:  "unrecognized",
			Message: "ok",
		}, nil
	}

	tokenID := strings.TrimPrefix(strings.TrimPrefix(data, "act:"), "rerun:")

	// 2. 运维白名单拦截
	if s.store != nil {
		userIDStr := strconv.FormatInt(in.FromID, 10)
		allowed, err := VerifyOperatorWhitelist(ctx, s.store.Settings(), "chatops.telegram.allowed_user_ids", userIDStr)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("chatops telegram whitelist failed", "error", err)
			}
			if in.CallbackQueryID != "" && s.feedback != nil {
				s.safeGo("telegram_callback_err", func() {
					s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ 安全配置异常，请联系管理员", true)
				})
			}
			return &Outcome{
				Status:  StatusCorruptedConfig,
				Action:  data,
				Message: "安全配置异常",
				Err:     ErrSecurityConfigBroken,
			}, ErrSecurityConfigBroken
		}
		if !allowed {
			if in.CallbackQueryID != "" && s.feedback != nil {
				s.safeGo("telegram_callback_unauth", func() {
					s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ 仅受信任的维护者有权执行此操作", true)
				})
			}
			return &Outcome{
				Status:  StatusForbidden,
				Action:  data,
				Message: "仅受信任的维护者有权执行此操作",
				Err:     ErrOperatorUnauthorized,
			}, ErrOperatorUnauthorized
		}
	}

	// 3. 原子消费单次操作令牌
	if s.tokens == nil {
		return &Outcome{Status: StatusRetryableFailure, Action: data, Message: "服务不可用", Err: ErrStoreUnavailable}, ErrStoreUnavailable
	}
	action, err := s.tokens.Consume(ctx, tokenID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("telegram chatops token consumption failed", "token_prefix", MaskToken(tokenID), "error", err.Error())
		}
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_invalid", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ 操作已失效或已被执行", true)
			})
		}
		return &Outcome{
			Status:  StatusInvalidToken,
			Action:  data,
			Message: "操作已失效或已被执行",
			Err:     ErrInvalidToken,
		}, err
	}

	// 4. Actor 绑定校验
	userIDStr := strconv.FormatInt(in.FromID, 10)
	if !VerifyActorBinding(action, userIDStr) {
		_ = s.tokens.Release(ctx, tokenID)
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_actor_mismatch", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ 无权执行此操作", true)
			})
		}
		return &Outcome{
			Status:  StatusForbidden,
			Action:  action.Action,
			Message: "无权执行此操作",
			Err:     ErrActorMismatch,
		}, ErrActorMismatch
	}

	// 5. 驱动执行器
	outcome, execErr := s.dispatchExecutor(ctx, *action)
	if execErr != nil {
		if s.logger != nil {
			s.logger.Error("telegram chatops action execution failed", "action", action.Action, "error", execErr.Error())
		}
		if outcome.CanReleaseToken() {
			_ = s.tokens.Release(ctx, tokenID)
		}
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_failed", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ "+outcome.Message, true)
			})
		}
		return outcome, execErr
	}

	// 6. 依据终态细分反馈
	switch outcome.Status {
	case StatusAlreadyDone:
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_already_done", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "ℹ️ "+outcome.Message, false)
			})
		}
		return outcome, nil
	case StatusPending:
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_pending", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⏳ "+outcome.Message, true)
			})
		}
		return outcome, nil
	case StatusIndeterminate:
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_indeterminate", func() {
				s.feedback.AnswerCallback(context.Background(), in.CallbackQueryID, "⚠️ "+outcome.Message, true)
			})
		}
		return outcome, nil
	default:
		// StatusExecuted: 异步就地更新消息键盘与反馈
		if in.CallbackQueryID != "" && s.feedback != nil {
			s.safeGo("telegram_callback_success", func() {
				s.feedback.AnswerActionCallback(context.Background(), in.CallbackQueryID, outcome.Action)
			})
		}
		if in.ChatID != 0 && in.MessageID != 0 && len(in.InlineKeyboard) > 0 && s.feedback != nil {
			s.safeGo("telegram_edit_keyboard", func() {
				s.feedback.EditMessageReplyMarkup(context.Background(), in.ChatID, in.MessageID, data, outcome.Action, in.InlineKeyboard)
			})
		}
		return outcome, nil
	}
}

func (s *service) HandleFeishu(ctx context.Context, in FeishuCallbackRequest) (*Outcome, error) {
	tokenID := strings.TrimSpace(in.TokenID)
	if tokenID == "" {
		return &Outcome{
			Status:  StatusInvalidToken,
			Action:  "empty",
			Message: "未指定操作动作",
			Err:     errors.New("empty token id"),
		}, errors.New("empty token id")
	}

	// 1. 白名单检查
	if s.store != nil {
		allowed, err := VerifyOperatorWhitelist(ctx, s.store.Settings(), "chatops.feishu.allowed_user_ids", in.OperatorOpenID)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("chatops feishu whitelist failed", "error", err)
			}
			return &Outcome{
				Status:  StatusCorruptedConfig,
				Action:  "feishu_whitelist",
				Message: "⚠️ 安全配置异常，请联系管理员",
				Err:     ErrSecurityConfigBroken,
			}, ErrSecurityConfigBroken
		}
		if !allowed {
			return &Outcome{
				Status:  StatusForbidden,
				Action:  "feishu_whitelist",
				Message: "⚠️ 仅受信任的维护者有权执行此操作",
				Err:     ErrOperatorUnauthorized,
			}, ErrOperatorUnauthorized
		}
	}

	// 2. 消费 Token
	if s.tokens == nil {
		return &Outcome{Status: StatusRetryableFailure, Action: "consume", Message: "服务不可用", Err: ErrStoreUnavailable}, ErrStoreUnavailable
	}
	action, err := s.tokens.Consume(ctx, tokenID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("feishu chatops token consumption failed", "token_prefix", MaskToken(tokenID), "error", err.Error())
		}
		return &Outcome{
			Status:  StatusInvalidToken,
			Action:  "consume",
			Message: "Action expired or already performed",
			Err:     ErrInvalidToken,
		}, err
	}

	// 3. Actor 绑定
	if !VerifyActorBinding(action, in.OperatorOpenID) {
		_ = s.tokens.Release(ctx, tokenID)
		return &Outcome{
			Status:  StatusForbidden,
			Action:  action.Action,
			Message: "⚠️ 无权执行此操作",
			Err:     ErrActorMismatch,
		}, ErrActorMismatch
	}

	// 4. 驱动执行器
	outcome, execErr := s.dispatchExecutor(ctx, *action)
	if execErr != nil {
		if s.logger != nil {
			s.logger.Error("feishu chatops action execution failed", "action", action.Action, "error", execErr.Error())
		}
		if outcome.CanReleaseToken() {
			_ = s.tokens.Release(ctx, tokenID)
		}
		return outcome, execErr
	}

	return outcome, nil
}

func (s *service) HandleSlack(ctx context.Context, in SlackCallbackRequest) (*Outcome, error) {
	tokenID := strings.TrimSpace(in.TokenID)
	if tokenID == "" {
		return &Outcome{
			Status:  StatusInvalidToken,
			Action:  "empty",
			Message: "Missing action token",
			Err:     errors.New("empty token id"),
		}, errors.New("empty token id")
	}

	// 1. 白名单检查
	if s.store != nil {
		allowed, err := VerifyOperatorWhitelist(ctx, s.store.Settings(), "chatops.slack.allowed_user_ids", in.UserID)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("chatops slack whitelist failed", "error", err)
			}
			return &Outcome{
				Status:  StatusCorruptedConfig,
				Action:  "slack_whitelist",
				Message: "⚠️ Security configuration error. Please contact administrator.",
				Err:     ErrSecurityConfigBroken,
			}, ErrSecurityConfigBroken
		}
		if !allowed {
			return &Outcome{
				Status:  StatusForbidden,
				Action:  "slack_whitelist",
				Message: "⚠️ Only trusted maintainers are authorized to execute this action.",
				Err:     ErrOperatorUnauthorized,
			}, ErrOperatorUnauthorized
		}
	}

	// 2. 消费 Token
	if s.tokens == nil {
		return &Outcome{Status: StatusRetryableFailure, Action: "consume", Message: "Service unavailable", Err: ErrStoreUnavailable}, ErrStoreUnavailable
	}
	action, err := s.tokens.Consume(ctx, tokenID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("slack chatops token consumption failed", "token_prefix", MaskToken(tokenID), "error", err.Error())
		}
		return &Outcome{
			Status:  StatusInvalidToken,
			Action:  "consume",
			Message: "Action expired or already performed",
			Err:     ErrInvalidToken,
		}, err
	}

	// 3. Actor 绑定
	if !VerifyActorBinding(action, in.UserID) {
		_ = s.tokens.Release(ctx, tokenID)
		return &Outcome{
			Status:  StatusForbidden,
			Action:  action.Action,
			Message: "⚠️ Unauthorized action execution.",
			Err:     ErrActorMismatch,
		}, ErrActorMismatch
	}

	// 4. 驱动执行器
	outcome, execErr := s.dispatchExecutor(ctx, *action)
	if execErr != nil {
		if s.logger != nil {
			s.logger.Error("slack chatops action execution failed", "action", action.Action, "error", execErr.Error())
		}
		if outcome.CanReleaseToken() {
			_ = s.tokens.Release(ctx, tokenID)
		}
		return outcome, execErr
	}

	return outcome, nil
}
