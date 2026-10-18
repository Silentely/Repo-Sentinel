package sideeffect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// RemoteSucceededReceiptFailedError 表示远端操作已成功生效，但本地回执落库持久化失败。
// 遇到此类错误时，必须严格禁止重试以防重复执行远端动作，同时不可释放对应 Token。
type RemoteSucceededReceiptFailedError struct {
	Key        string
	ExternalID string
	Err        error
}

func (e *RemoteSucceededReceiptFailedError) Error() string {
	if e == nil {
		return "remote succeeded but receipt persist failed"
	}
	return fmt.Sprintf("side_effect succeeded externally (id=%s) but receipt persist failed for %s: %v", e.ExternalID, e.Key, e.Err)
}

func (e *RemoteSucceededReceiptFailedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsRemoteSucceededReceiptFailed 判定是否为远端已成功但回执持久化失败的错误。
func IsRemoteSucceededReceiptFailed(err error) bool {
	if err == nil {
		return false
	}
	var target *RemoteSucceededReceiptFailedError
	return errors.As(err, &target) || strings.Contains(strings.ToLower(err.Error()), "succeeded externally but receipt persist failed")
}

// UnknownReceiptPersistError 表示执行时遇到网络在途不确定错误，且保存 unknown 回执时也失败。
type UnknownReceiptPersistError struct {
	NetworkErr error
	PersistErr error
	Key        string
}

func (e *UnknownReceiptPersistError) Error() string {
	if e == nil {
		return "unknown receipt persist failed"
	}
	return fmt.Sprintf("side_effect network unknown (%v) and unknown-receipt persist failed for %s: %v", e.NetworkErr, e.Key, e.PersistErr)
}

func (e *UnknownReceiptPersistError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.NetworkErr
}

// IsNetworkUnknownError 判断是否为在途网络不确定性错误（超时、连接重置、读取 EOF、入站上下文取消等）。
// 遇到此类错误时，远端服务可能已经执行成功，因此必须保留回执，禁止直接删除重试。
func IsNetworkUnknownError(err error) bool {
	if err == nil {
		return false
	}
	var upe *UnknownReceiptPersistError
	if errors.As(err, &upe) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
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
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "canceled") ||
		strings.Contains(msg, "cancelled") ||
		strings.Contains(msg, "unknown-receipt persist failed") ||
		strings.Contains(msg, "network unknown") ||
		msg == "eof" || strings.HasSuffix(msg, ": eof") {
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

// ExecutionDetail 结构化描述副作用回执执行的具体结果。
type ExecutionDetail struct {
	State      ReceiptState
	IsNew      bool
	ExternalID string
}

// ExecuteWithReceiptDetail 外部副作用生命周期执行器，返回细粒度结构化执行状态与结果。
func ExecuteWithReceiptDetail(
	ctx context.Context,
	settings store.SettingsStore,
	effectType, repoID, subjectNumber, revision, target string,
	fn func() (string, error),
) (*ExecutionDetail, error) {
	if settings == nil {
		externalID, err := fn()
		if err != nil {
			return nil, err
		}
		return &ExecutionDetail{
			State:      ReceiptSucceeded,
			IsNew:      true,
			ExternalID: externalID,
		}, nil
	}

	key := SideEffectReceiptKey(effectType, repoID, subjectNumber, revision, target)
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
		return executeSideEffectDetail(ctx, settings, key, effectType, target, fn, now)
	}
	if !errors.Is(err, store.ErrConflict) {
		return nil, fmt.Errorf("side_effect receipt create failed for %s: %w", key, err)
	}

	// 已存在回执：读出状态判断。
	existing, getErr := settings.Get(ctx, key)
	if getErr != nil {
		return nil, fmt.Errorf("side_effect receipt read failed for %s: %w", key, getErr)
	}
	var rec SideEffectReceipt
	if json.Unmarshal(existing.ValueJSON, &rec) != nil {
		return nil, fmt.Errorf("side_effect receipt malformed for %s", key)
	}
	switch {
	case rec.State == ReceiptSucceeded:
		return &ExecutionDetail{
			State:      ReceiptSucceeded,
			IsNew:      false,
			ExternalID: rec.ExternalID,
		}, nil
	case rec.State == ReceiptUnknown:
		return &ExecutionDetail{
			State:      ReceiptUnknown,
			IsNew:      false,
			ExternalID: rec.ExternalID,
		}, nil
	case rec.State == ReceiptPending:
		// 保守阻断：已存在 pending 回执时，严格视作在途或结果待对账，杜绝自动删除重放导致重复副作用
		return &ExecutionDetail{
			State:      ReceiptPending,
			IsNew:      false,
			ExternalID: "",
		}, nil
	default:
		return nil, fmt.Errorf("side_effect receipt in unknown state %q for %s", rec.State, key)
	}
}

// ExecuteWithReceipt 外部副作用显式生命周期回执防刷执行器（保持旧签名兼容）。
func ExecuteWithReceipt(
	ctx context.Context,
	settings store.SettingsStore,
	effectType, repoID, subjectNumber, revision, target string,
	fn func() (string, error),
) error {
	_, err := ExecuteWithReceiptDetail(ctx, settings, effectType, repoID, subjectNumber, revision, target, fn)
	return err
}

// executeSideEffectDetail 在已写入 pending 回执后执行外部调用，并按其结果推进回执状态。
func executeSideEffectDetail(
	ctx context.Context,
	settings store.SettingsStore,
	key string,
	effectType, target string,
	fn func() (string, error),
	createdAt time.Time,
) (*ExecutionDetail, error) {
	externalID, execErr := fn()

	// 终态回执持久化必须脱离入站 ctx 取消，避免客户端断开导致无法保存终态
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

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
		if _, err := settings.Upsert(persistCtx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       key,
			ValueJSON: successJSON,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "side_effect_executor",
		}); err != nil {
			// 外部副作用已成功但本地持久化回执失败：构造类型化错误，禁止重试
			detail := &ExecutionDetail{
				State:      ReceiptSucceeded,
				IsNew:      true,
				ExternalID: externalID,
			}
			retErr := &RemoteSucceededReceiptFailedError{
				Key:        key,
				ExternalID: externalID,
				Err:        err,
			}
			return detail, retErr
		}
		return &ExecutionDetail{
			State:      ReceiptSucceeded,
			IsNew:      true,
			ExternalID: externalID,
		}, nil
	}

	if IsPreSendError(execErr) {
		// 前置参数校验失败（尚未发出网络请求）：安全删除回执，允许修正后立即重试。
		_ = settings.Delete(persistCtx, key)
		return nil, execErr
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
		if _, err := settings.Upsert(persistCtx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       key,
			ValueJSON: unknownJSON,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "side_effect_executor",
		}); err != nil {
			detail := &ExecutionDetail{
				State:      ReceiptUnknown,
				IsNew:      true,
				ExternalID: externalID,
			}
			retErr := &UnknownReceiptPersistError{
				NetworkErr: execErr,
				PersistErr: err,
				Key:        key,
			}
			return detail, retErr
		}
		return &ExecutionDetail{
			State:      ReceiptUnknown,
			IsNew:      true,
			ExternalID: externalID,
		}, execErr
	}

	// 明确远端错误（如 4xx 业务拒绝）：删除 pending 回执以允许重试。
	_ = settings.Delete(persistCtx, key)
	return nil, execErr
}
