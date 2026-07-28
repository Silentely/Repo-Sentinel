package store

import "errors"

var (
	// ErrConflict 表示唯一约束或状态约束冲突。
	ErrConflict = errors.New("store conflict")
	// ErrNotFound 表示请求的领域记录不存在。
	ErrNotFound = errors.New("store record not found")
	// ErrDatabaseUnavailable 表示无法打开或连通数据库（公开错误根）。
	ErrDatabaseUnavailable = errors.New("database unavailable")
	// ErrMigrationFailed 表示迁移失败（公开错误根）。
	ErrMigrationFailed = errors.New("database migration failed")
)

var (
	errDatabaseUnavailable = ErrDatabaseUnavailable
	errMigrationFailed     = ErrMigrationFailed
	errDatabaseOperation   = errors.New("database operation failed")
)

// OpenError 是打开数据库时的安全诊断：Error() 永不包含连接串/密码，
// 仅 Kind 与可选的安全 Reason 供 CLI/日志使用。
type OpenError struct {
	Kind   string // 稳定分类：driver / dsn / connect / migrate / unknown
	Reason string // 不含凭据的简短说明
	cause  error
}

func (e *OpenError) Error() string {
	if e == nil {
		return "database unavailable"
	}
	if e.Reason != "" {
		return "database unavailable: " + e.Reason
	}
	return "database unavailable"
}

func (e *OpenError) Unwrap() error {
	if e == nil {
		return nil
	}
	root := errDatabaseUnavailable
	if e.Kind == "migrate" {
		root = errMigrationFailed
	}
	if e.cause == nil || e.cause == root {
		return root
	}
	return errors.Join(root, e.cause)
}

func (e *OpenError) ErrorCode() string {
	if e != nil && e.Kind == "migrate" {
		return "migration_failed"
	}
	return "database_unavailable"
}

func newOpenError(kind, reason string, cause error) *OpenError {
	return &OpenError{Kind: kind, Reason: reason, cause: cause}
}
