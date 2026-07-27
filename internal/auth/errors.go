package auth

type codedError string

func (e codedError) Error() string {
	return string(e)
}

func (e codedError) ErrorCode() string {
	return string(e)
}

var (
	// ErrInvalidCredentials 对未知账号、错误密码与损坏密码哈希使用同一安全错误。
	ErrInvalidCredentials = codedError("invalid_credentials")
	// ErrConflict 表示唯一管理员或状态发生冲突。
	ErrConflict = codedError("conflict")
	// ErrValidationFailed 表示调用输入未通过安全校验。
	ErrValidationFailed = codedError("validation_failed")
	// ErrUnauthorized 统一表示 Session 缺失、无效、未知或已过期。
	ErrUnauthorized = codedError("unauthorized")
	// ErrCSRFFailed 统一表示双提交 CSRF 校验缺失或不匹配。
	ErrCSRFFailed = codedError("csrf_failed")
)
