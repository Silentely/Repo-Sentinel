package rules

import (
	"context"

	"github.com/Silentely/Repo-Sentinel/internal/sideeffect"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type ReceiptState = sideeffect.ReceiptState

const (
	ReceiptPending   ReceiptState = sideeffect.ReceiptPending
	ReceiptSucceeded ReceiptState = sideeffect.ReceiptSucceeded
	ReceiptUnknown   ReceiptState = sideeffect.ReceiptUnknown
)

var (
	ErrSideEffectAlreadyExecuted = sideeffect.ErrSideEffectAlreadyExecuted
	ErrPreSend                   = sideeffect.ErrPreSend
)

type (
	PreSendError                      = sideeffect.PreSendError
	RemoteSucceededReceiptFailedError = sideeffect.RemoteSucceededReceiptFailedError
	UnknownReceiptPersistError        = sideeffect.UnknownReceiptPersistError
	ExecutionDetail                   = sideeffect.ExecutionDetail
	SideEffectReceipt                 = sideeffect.SideEffectReceipt
)

var (
	MarkPreSendError               = sideeffect.MarkPreSendError
	IsPreSendError                 = sideeffect.IsPreSendError
	IsRemoteSucceededReceiptFailed = sideeffect.IsRemoteSucceededReceiptFailed
	IsNetworkUnknownError          = sideeffect.IsNetworkUnknownError
	SideEffectReceiptKey           = sideeffect.SideEffectReceiptKey
	ExecuteWithReceiptDetail       = sideeffect.ExecuteWithReceiptDetail
)

// ExecuteWithReceipt 转发至 internal/sideeffect.ExecuteWithReceipt，保证平滑向后兼容。
func ExecuteWithReceipt(
	ctx context.Context,
	settings store.SettingsStore,
	effectType, repoID, subjectNumber, revision, target string,
	fn func() (string, error),
) error {
	return sideeffect.ExecuteWithReceipt(ctx, settings, effectType, repoID, subjectNumber, revision, target, fn)
}
