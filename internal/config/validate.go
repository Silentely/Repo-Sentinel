package config

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const validationFailedCode = "validation_failed"

// maxAITimeout AI 请求超时上界：过大超时会让单条 webhook 后台处理占用
// 32 并发槽位过久（processBudget = AI timeout + 10s），设置页数字钳制同步上限。
const maxAITimeout = 10 * time.Minute

// ValidationError 表示可稳定识别且不包含配置原文的校验错误。
type ValidationError struct {
	Field   string
	Message string
}

// Error 返回固定错误码、字段名和安全说明，不回显失败输入。
func (e *ValidationError) Error() string {
	if e == nil {
		return validationFailedCode
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", validationFailedCode, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", validationFailedCode, e.Field, e.Message)
}

// ErrorCode 返回供 API 与 CLI 映射使用的稳定错误码。
func (*ValidationError) ErrorCode() string {
	return validationFailedCode
}

func newValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// Validate 校验安全敏感且必须在启动前确定的配置。
func (cfg Config) Validate() error {
	driver := strings.TrimSpace(cfg.Database.Driver)
	switch driver {
	case "sqlite":
	case "postgres":
		if strings.TrimSpace(cfg.Database.URL) == "" {
			return newValidationError("database.url", "is required for postgres")
		}
	default:
		return newValidationError("database.driver", "must be sqlite or postgres")
	}
	// SQLite 强制单连接（见 store.openDatabase）：显式配置更大的连接池会被静默忽略，
	// 启动前直接拒绝该组合，避免「配置了但未生效」的漂移。
	if driver == "sqlite" && cfg.Database.MaxOpenConns > 1 {
		return newValidationError("database.max_open_conns", "is only effective with postgres (sqlite enforces a single connection)")
	}
	if driver == "sqlite" && cfg.Database.MaxIdleConns > 1 {
		return newValidationError("database.max_idle_conns", "is only effective with postgres (sqlite enforces a single connection)")
	}

	if _, _, err := net.SplitHostPort(strings.TrimSpace(cfg.HTTP.Addr)); err != nil {
		return newValidationError("http.addr", "must contain a valid host and port")
	}
	if !validPublicBaseURL(cfg.HTTP.PublicBaseURL) {
		return newValidationError("http.public_base_url", "must use HTTPS except for localhost or loopback HTTP")
	}
	for _, proxy := range cfg.HTTP.TrustedProxies {
		trimmed := strings.TrimSpace(proxy)
		if trimmed == "" {
			return newValidationError("http.trusted_proxies", "must not contain empty elements")
		}
		if strings.Contains(trimmed, "/") {
			if _, _, err := net.ParseCIDR(trimmed); err != nil {
				return newValidationError("http.trusted_proxies", "must contain valid IP addresses or CIDR blocks")
			}
		} else {
			if net.ParseIP(trimmed) == nil {
				return newValidationError("http.trusted_proxies", "must contain valid IP addresses or CIDR blocks")
			}
		}
	}

	hasUsername := cfg.Admin.Username != ""
	hasPassword := cfg.Admin.Password.Reveal() != ""
	if hasUsername != hasPassword {
		return newValidationError("admin", "username and password must be provided together")
	}

	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return newValidationError("logging.level", "must be debug, info, warn, or error")
	}
	switch cfg.Logging.Format {
	case "json", "text":
	default:
		return newValidationError("logging.format", "must be json or text")
	}

	if !validEncryptionKey(cfg.Encryption.CurrentKey) {
		return newValidationError("encryption.current_key", "must decode from base64 or hex to exactly 32 bytes")
	}
	if !validEncryptionKey(cfg.Encryption.PreviousKey) {
		return newValidationError("encryption.previous_key", "must decode from base64 or hex to exactly 32 bytes")
	}
	if cfg.UpdateCheck.Enabled {
		u := strings.TrimSpace(cfg.UpdateCheck.URL)
		if u != "" {
			parsed, err := url.Parse(u)
			if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
				return newValidationError("update_check.url", "must be an https URL without userinfo")
			}
		}
	}
	if cfg.Aggregation.Window < 0 {
		return newValidationError("aggregation.window", "must be >= 0")
	}
	if cfg.Aggregation.BurstThreshold < 0 {
		return newValidationError("aggregation.burst_threshold", "must be >= 0")
	}
	if cfg.Aggregation.BurstWindow < 0 {
		return newValidationError("aggregation.burst_window", "must be >= 0")
	}
	if cfg.AI.Enabled {
		if strings.TrimSpace(cfg.AI.APIKey.Reveal()) == "" {
			return newValidationError("ai.api_key", "is required when ai.enabled is true")
		}
		if cfg.AI.Timeout < 0 || cfg.AI.Timeout > maxAITimeout {
			return newValidationError("ai.timeout", fmt.Sprintf("must be between 0 and %s", maxAITimeout))
		}
		// max_tokens 为 0 表示使用默认 800（客户端在使用点回退），仅拒绝负值。
		if cfg.AI.MaxTokens < 0 {
			return newValidationError("ai.max_tokens", "must be >= 0")
		}
		base := strings.TrimSpace(cfg.AI.BaseURL)
		if base != "" {
			parsed, err := url.Parse(base)
			// 与 httpapi 校验一致：拒绝 userinfo（URL 内嵌凭据为坏实践）。
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
				return newValidationError("ai.base_url", "must be an http(s) URL")
			}
		}
	}
	return nil
}

func validPublicBaseURL(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" {
		return false
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.ToLower(parsed.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

func validEncryptionKey(key Secret) bool {
	raw := key.Reveal()
	if raw == "" {
		return true
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && len(decoded) == 32 {
		return true
	}
	rawStd, err := base64.RawStdEncoding.DecodeString(raw)
	if err == nil && len(rawStd) == 32 {
		return true
	}
	hexDecoded, err := hex.DecodeString(raw)
	return err == nil && len(hexDecoded) == 32
}
