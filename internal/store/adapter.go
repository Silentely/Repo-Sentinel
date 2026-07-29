package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
)

func mapStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case entclient.IsNotFound(err):
		return ErrNotFound
	case entclient.IsConstraintError(err):
		return ErrConflict
	default:
		// 保留原始错误信息，便于排查；外层调用方仍可用 errors.Is 匹配 errDatabaseOperation。
		return fmt.Errorf("%w: %w", errDatabaseOperation, err)
	}
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
