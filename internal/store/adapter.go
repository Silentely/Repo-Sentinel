package store

import (
	"context"
	"encoding/json"
	"errors"

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
		return errDatabaseOperation
	}
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
