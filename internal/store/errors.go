package store

import "errors"

var (
	// ErrConflict 表示唯一约束或状态约束冲突。
	ErrConflict = errors.New("store conflict")
	// ErrNotFound 表示请求的领域记录不存在。
	ErrNotFound = errors.New("store record not found")
)

var (
	errDatabaseUnavailable = errors.New("database unavailable")
	errMigrationFailed     = errors.New("database migration failed")
	errDatabaseOperation   = errors.New("database operation failed")
)
