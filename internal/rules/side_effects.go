package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

type ReceiptState string

const (
	ReceiptPending   ReceiptState = "pending"
	ReceiptSucceeded ReceiptState = "succeeded"
	ReceiptUnknown   ReceiptState = "unknown"
)

var (
	ErrSideEffectAlreadyExecuted = errors.New("side_effect_already_executed")
	ErrPreSend                   = errors.New("pre_send_error")
)

// PreSendError 表示在尚未发出网络请求的前置阶段发生的错误。
type PreSendError struct {
	Err error
}

func (e *PreSendError) Error() string {
	if e == nil || e.Err == nil {
		return "pre_send_error"
	}
	return fmt.Sprintf("pre_send_error: %v", e.Err)
}

func (e *PreSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MarkPreSendError 将错误包装为前置错误（表示尚未发出外部网络调用）。
func MarkPreSendError(err error) error {
	if err == nil {
		return nil
	}
	return &PreSendError{Err: err}
}

// IsPreSendError 判断是否为外部网络请求发送前的错误。
func IsPreSendError(err error) bool {
	if err == nil {
		return false
	}
	var pse *PreSendError
	return errors.As(err, &pse) || errors.Is(err, ErrPreSend)
}

// IsNetworkUnknownError 判断是否为在途网络不确定性错误（超时、连接重置、读取 EOF 等）。
// 遇到此类错误时，远端服务可能已经执行成功，因此必须保留回执，禁止直接删除重试。
func IsNetworkUnknownError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout()) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "broken pipe") {
		return true
	}
	return false
}

// SideEffectReceipt 记录外部副作用执行状态。
type SideEffectReceipt struct {
	State      ReceiptState `json:"state"`
	EffectType string       `json:"effect_type"`
	Target     string       `json:"target,omitempty"`
	ExternalID string       `json:"external_id,omitempty"`
	Error      string       `json:"error,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// SideEffectReceiptKey 生成规范化的副作用回执键。
func SideEffectReceiptKey(effectType, repoID, subjectNumber, revision, target string) string {
	return fmt.Sprintf("side_effect:%s:%s:%s:%s:%s", effectType, repoID, subjectNumber, revision, target)
}

// ExecuteWithReceipt 外部副作用显式生命周期回执防刷执行器。
// fn 返回 externalID (如 GitHub comment ID 或 run ID) 与 error。
//
// 关键约定：任何无法判定的回执状态都必须返回错误（供调用方留痕），
// 绝不能返回 nil 把「未执行」伪装成「已成功」；陈旧 pending 回执（执行者大概率已崩溃）
// 会先回收再重试一次，避免副作用被永久阻塞。
func ExecuteWithReceipt(
	ctx context.Context,
	settings store.SettingsStore,
	effectType, repoID, subjectNumber, revision, target string,
	fn func() (string, error),
) error {
	if settings == nil {
		_, err := fn()
		return err
	}

	key := SideEffectReceiptKey(effectType, repoID, subjectNumber, revision, target)
	// 最多允许一次「陈旧 pending 回收重试」，防止异常回执导致无限循环。
	const maxReclaimAttempts = 2
	for attempt := 0; attempt < maxReclaimAttempts; attempt++ {
		now := time.Now().UTC()
		initialReceipt := SideEffectReceipt{
			State:      ReceiptPending,
			EffectType: effectType,
			Target:     target,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		rawJSON, _ := json.Marshal(initialReceipt)

		// Step 1: 原子写入 pending 记录（唯一键约束即并发互斥）。
		_, err := settings.Create(ctx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       key,
			ValueJSON: rawJSON,
			UpdatedAt: now,
			UpdatedBy: "side_effect_executor",
		})
		if err == nil {
			return executeSideEffect(ctx, settings, key, effectType, target, fn, now)
		}
		if !errors.Is(err, store.ErrConflict) {
			// 非冲突错误（数据库抖动等）不能当作成功：返回错误以避免副作用被静默丢弃。
			return fmt.Errorf("side_effect receipt create failed for %s: %w", key, err)
		}

		// 已存在回执：读出状态判断。
		existing, getErr := settings.Get(ctx, key)
		if getErr != nil {
			return fmt.Errorf("side_effect receipt read failed for %s: %w", key, getErr)
		}
		var rec SideEffectReceipt
		if json.Unmarshal(existing.ValueJSON, &rec) != nil {
			return fmt.Errorf("side_effect receipt malformed for %s", key)
		}
		switch {
		case rec.State == ReceiptSucceeded || rec.State == ReceiptUnknown:
			// 已成功或网络不确定态：幂等跳过，杜绝二次副作用。
			return nil
		case rec.State == ReceiptPending && now.Sub(rec.UpdatedAt) < 2*time.Minute:
			// 仍在租约期内：视为正在执行中，跳过。
			return nil
		case rec.State == ReceiptPending:
			// 陈旧 pending：原执行者大概率已崩溃，回收后重试一次。
			if delErr := settings.Delete(ctx, key); delErr != nil && !errors.Is(delErr, store.ErrNotFound) {
				return fmt.Errorf("side_effect stale receipt reclaim failed for %s: %w", key, delErr)
			}
			continue
		default:
			return fmt.Errorf("side_effect receipt in unknown state %q for %s", rec.State, key)
		}
	}
	return fmt.Errorf("side_effect receipt reclaim retry exhausted for %s", key)
}

// executeSideEffect 在已写入 pending 回执后执行外部调用，并按其结果推进回执状态。
func executeSideEffect(ctx context.Context, settings store.SettingsStore, key string, effectType, target string, fn func() (string, error), createdAt time.Time) error {
	externalID, execErr := fn()
	if execErr == nil {
		successReceipt := SideEffectReceipt{
			State:      ReceiptSucceeded,
			EffectType: effectType,
			Target:     target,
			ExternalID: externalID,
			CreatedAt:  createdAt,
			UpdatedAt:  time.Now().UTC(),
		}
		successJSON, _ := json.Marshal(successReceipt)
		_, _ = settings.Upsert(ctx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       key,
			ValueJSON: successJSON,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "side_effect_executor",
		})
		return nil
	}

	if IsPreSendError(execErr) {
		// 前置参数校验失败（尚未发出网络请求）：安全删除回执，允许修正后立即重试。
		_ = settings.Delete(ctx, key)
		return execErr
	}

	if IsNetworkUnknownError(execErr) {
		// 网络不确定态：远端可能已生效，严格保留回执为 unknown，绝不删除，杜绝重复调用。
		unknownReceipt := SideEffectReceipt{
			State:      ReceiptUnknown,
			EffectType: effectType,
			Target:     target,
			Error:      execErr.Error(),
			CreatedAt:  createdAt,
			UpdatedAt:  time.Now().UTC(),
		}
		unknownJSON, _ := json.Marshal(unknownReceipt)
		_, _ = settings.Upsert(ctx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       key,
			ValueJSON: unknownJSON,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "side_effect_executor",
		})
		return execErr
	}

	// 明确远端错误（如 4xx 业务拒绝）：删除 pending 回执以允许重试。
	_ = settings.Delete(ctx, key)
	return execErr
}
