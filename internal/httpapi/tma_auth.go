package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// ErrTMABotTokenUnavailable 是 Bot Token 解析/解密失败时的统一哨兵错误，
// 确保底层解密错误文本和密钥材料绝不泄露到错误链或日志中。
var ErrTMABotTokenUnavailable = errors.New("telegram bot token unavailable")

// TelegramUser 描述从 initData 解析出的 Telegram 用户信息。
type TelegramUser struct {
	ID              int64  `json:"id"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	Username        string `json:"username"`
	LanguageCode    string `json:"language_code"`
	AllowsWriteToPM bool   `json:"allows_write_to_pm,omitempty"`
}

type tmaUserCtxKey struct{}

// ContextWithTelegramUser 将解析后的 TelegramUser 存入 Context。
func ContextWithTelegramUser(ctx context.Context, u *TelegramUser) context.Context {
	return context.WithValue(ctx, tmaUserCtxKey{}, u)
}

// GetTelegramUser 从 Context 中读取 TelegramUser。
func GetTelegramUser(ctx context.Context) (*TelegramUser, bool) {
	if ctx == nil {
		return nil, false
	}
	u, ok := ctx.Value(tmaUserCtxKey{}).(*TelegramUser)
	return u, ok && u != nil
}

// ValidateTelegramInitData 校验 Telegram Mini App initData HMAC-SHA256 签名，
// 遵循官方规范：secret_key = HMAC_SHA256(key="WebAppData", msg=bot_token)。
// 校验通过后返回解析出的 TelegramUser。
func ValidateTelegramInitData(initData string, token string, maxAge time.Duration) (*TelegramUser, error) {
	if strings.TrimSpace(initData) == "" {
		return nil, errors.New("empty initData")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("empty bot token")
	}

	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, fmt.Errorf("invalid initData query string: %w", err)
	}

	hashHex := values.Get("hash")
	if hashHex == "" {
		return nil, errors.New("missing hash in initData")
	}
	expectedHash, err := hex.DecodeString(hashHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hash hex: %w", err)
	}

	authDateStr := values.Get("auth_date")
	if authDateStr == "" {
		return nil, errors.New("missing auth_date in initData")
	}
	authDateInt, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid auth_date: %w", err)
	}

	authTime := time.Unix(authDateInt, 0)
	now := time.Now()
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	if now.Sub(authTime) > maxAge {
		return nil, errors.New("auth_date is expired")
	}
	// 允许最多 1 分钟时钟漂移，拒绝未来时间伪造
	if authTime.After(now.Add(1 * time.Minute)) {
		return nil, errors.New("auth_date is in the future")
	}

	var keys []string
	for k := range values {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(pairs, "\n")

	// 官方规范：以常量 "WebAppData" 作为 HMAC Key，Bot Token 作为 Message 派生密钥
	macKey := hmac.New(sha256.New, []byte("WebAppData"))
	macKey.Write([]byte(token))
	secretKey := macKey.Sum(nil)

	macData := hmac.New(sha256.New, secretKey)
	macData.Write([]byte(dataCheckString))
	calcHash := macData.Sum(nil)

	if !hmac.Equal(calcHash, expectedHash) {
		return nil, errors.New("signature mismatch")
	}

	userRaw := values.Get("user")
	if userRaw == "" {
		return nil, errors.New("missing user in initData")
	}
	var user TelegramUser
	if err := json.Unmarshal([]byte(userRaw), &user); err != nil {
		return nil, fmt.Errorf("invalid user json: %w", err)
	}
	if user.ID == 0 {
		return nil, errors.New("invalid telegram user id")
	}

	return &user, nil
}

type botTokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

var (
	botTokenMu    sync.RWMutex
	botTokenCache = make(map[string]botTokenCacheEntry)
)

const (
	botTokenCacheKey = "tma:resolved_bot_token"
	botTokenCacheTTL = 5 * time.Minute
)

// InvalidateResolvedBotTokenCache 清理解析出的 Bot Token 缓存。
func InvalidateResolvedBotTokenCache() {
	botTokenMu.Lock()
	delete(botTokenCache, botTokenCacheKey)
	botTokenMu.Unlock()
}

func cacheResolvedBotToken(token string) {
	if token == "" {
		return
	}
	botTokenMu.Lock()
	botTokenCache[botTokenCacheKey] = botTokenCacheEntry{
		token:     token,
		expiresAt: time.Now().Add(botTokenCacheTTL),
	}
	botTokenMu.Unlock()
}

// ResolveBotToken 按三级优先级解析 Bot Token：
//  1. 数据库启用的 Telegram 渠道 (st.Channels().GetEnabledByType(..., ChannelTelegram))：
//     已启用 DB 渠道为唯一真源！
//     - 若 SecretEnvelope 为空，显式 fail-closed 记录错误并返回 ErrTMABotTokenUnavailable，绝不静默回退！
//     - 若 SecretEnvelope 解密失败，显式 fail-closed 只记录 channel_id 与稳定错误码，绝不打印/包装原始错误，返回 ErrTMABotTokenUnavailable！
//  2. 若渠道未找到 (store.ErrNotFound)，调用 chatops.ResolveTelegramBotToken(ctx, st)；
//  3. 静态配置 Token (cfgToken)；
//
// 解析成功后带 5 分钟读写锁内存缓存。
func ResolveBotToken(ctx context.Context, st store.Store, kr cryptox.KeyRing, cfgToken string) (string, error) {
	botTokenMu.RLock()
	if entry, ok := botTokenCache[botTokenCacheKey]; ok && time.Now().Before(entry.expiresAt) {
		botTokenMu.RUnlock()
		return entry.token, nil
	}
	botTokenMu.RUnlock()

	// 1st tier: check st.Channels().GetEnabledByType
	if st != nil && st.Channels() != nil {
		ch, err := st.Channels().GetEnabledByType(ctx, store.ChannelTelegram)
		if err == nil {
			if ch.SecretEnvelope == "" {
				slog.Default().Error(
					"telegram bot token missing: channel enabled without secret envelope",
					"error_code", "telegram_channel_secret_missing",
					"channel_id", ch.ID,
				)
				return "", ErrTMABotTokenUnavailable
			}
			res, decErr := kr.Decrypt(ctx, ch.SecretEnvelope, []byte(notify.AAD))
			if decErr != nil {
				// 严格安全边界：只记 channel_id 与稳定错误码，绝对不记录 decErr，返回哨兵错误
				slog.Default().Error(
					"telegram bot token decrypt failed",
					"error_code", "telegram_bot_token_decrypt_failed",
					"channel_id", ch.ID,
				)
				return "", ErrTMABotTokenUnavailable
			}
			token := string(res.Plaintext)
			if token == "" {
				slog.Default().Error(
					"telegram bot token empty after decryption",
					"error_code", "telegram_bot_token_empty",
					"channel_id", ch.ID,
				)
				return "", ErrTMABotTokenUnavailable
			}
			cacheResolvedBotToken(token)
			return token, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.Default().Error(
				"failed to query telegram channel",
				"error_code", "telegram_channel_query_failed",
			)
			return "", ErrTMABotTokenUnavailable
		}
	}

	// 2nd tier: chatops.ResolveTelegramBotToken
	if resolved := chatops.ResolveTelegramBotToken(ctx, st); resolved != "" {
		cacheResolvedBotToken(resolved)
		return resolved, nil
	}

	// 3rd tier: cfgToken
	if strings.TrimSpace(cfgToken) != "" {
		token := strings.TrimSpace(cfgToken)
		cacheResolvedBotToken(token)
		return token, nil
	}

	return "", nil
}

// isTMAUserAllowed 校验 Telegram 操作者白名单，单一真源：chatops.telegram.allowed_user_ids 系统设置。
func isTMAUserAllowed(ctx context.Context, st store.Store, userID int64) bool {
	if st == nil || st.Settings() == nil {
		return false
	}
	settingStr, err := chatops.ReadSecureConfig(ctx, st.Settings(), "chatops.telegram.allowed_user_ids")
	if err != nil || strings.TrimSpace(settingStr) == "" {
		return false
	}
	userIDStr := strconv.FormatInt(userID, 10)
	ok, err := chatops.VerifyOperatorWhitelist(ctx, st.Settings(), "chatops.telegram.allowed_user_ids", userIDStr)
	return err == nil && ok
}

func tmaRemoteIP(r *http.Request) string {
	if ip := remoteIPFromContext(r.Context()); ip != "" {
		return ip
	}
	return clientIPFromRemoteAddr(r.RemoteAddr)
}

func tmaRequestID(r *http.Request) string {
	if id := requestIDFromContext(r.Context()); id != "" {
		return id
	}
	return r.Header.Get("X-Request-ID")
}

func auditMetadataJSON(fields map[string]any) []byte {
	b, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	return b
}

func appendTMAAudit(ctx context.Context, st store.Store, logger *slog.Logger, entry store.AuditLog) {
	if st == nil || st.Audits() == nil {
		return
	}
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = ulid.Make().String()
	}
	if len(entry.MetadataJSON) > 0 {
		entry.MetadataJSON = []byte(notify.RedactSensitiveSecrets(string(entry.MetadataJSON)))
	}
	if _, err := st.Audits().Append(ctx, entry); err != nil && logger != nil {
		logger.Warn("audit log append failed",
			"error_code", "audit_append_failed",
			"action", entry.Action,
			"target_type", entry.TargetType,
			"target_id", entry.TargetID,
			"error", err.Error(),
		)
	}
}

// TMAAuthMiddleware 返回 Telegram Web Mini App 鉴权中间件。
// 校验 X-Telegram-Init-Data 请求头、验签及白名单准入，Fail-Closed。
func TMAAuthMiddleware(
	st store.Store,
	kr cryptox.KeyRing,
	cfgToken string,
	logger *slog.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := tmaRequestID(r)
			remIP := tmaRemoteIP(r)

			initData := r.Header.Get("X-Telegram-Init-Data")
			if strings.TrimSpace(initData) == "" {
				appendTMAAudit(r.Context(), st, logger, store.AuditLog{
					Action:    "tma.auth.denied",
					ActorType: "telegram/tma",
					MetadataJSON: auditMetadataJSON(map[string]any{
						"reason":     "missing_init_data_header",
						"error_code": errorCodeUnauthorized,
						"request_id": reqID,
						"remote_ip":  remIP,
					}),
				})
				writeAPIErrorResponse(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
				return
			}

			token, err := ResolveBotToken(r.Context(), st, kr, cfgToken)
			if err != nil {
				if logger != nil {
					logger.Error("resolve bot token failed during tma auth", "error_code", "tma_bot_token_unavailable")
				}
				appendTMAAudit(r.Context(), st, logger, store.AuditLog{
					Action:    "tma.auth.denied",
					ActorType: "telegram/tma",
					MetadataJSON: auditMetadataJSON(map[string]any{
						"reason":     "bot_token_unavailable",
						"error_code": errorCodeServiceUnavailable,
						"request_id": reqID,
						"remote_ip":  remIP,
					}),
				})
				writeAPIErrorResponse(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
				return
			}
			if token == "" {
				if logger != nil {
					logger.Error("bot token not configured for tma auth", "error_code", "tma_bot_token_not_configured")
				}
				appendTMAAudit(r.Context(), st, logger, store.AuditLog{
					Action:    "tma.auth.denied",
					ActorType: "telegram/tma",
					MetadataJSON: auditMetadataJSON(map[string]any{
						"reason":     "bot_token_not_configured",
						"error_code": errorCodeUnauthorized,
						"request_id": reqID,
						"remote_ip":  remIP,
					}),
				})
				writeAPIErrorResponse(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
				return
			}

			user, valErr := ValidateTelegramInitData(initData, token, 24*time.Hour)
			if valErr != nil {
				if logger != nil {
					logger.Warn("tma auth validation failed", "error_code", "signature_verification_failed")
				}
				appendTMAAudit(r.Context(), st, logger, store.AuditLog{
					Action:    "tma.auth.denied",
					ActorType: "telegram/tma",
					MetadataJSON: auditMetadataJSON(map[string]any{
						"reason":     "signature_verification_failed",
						"error_code": errorCodeUnauthorized,
						"request_id": reqID,
						"remote_ip":  remIP,
					}),
				})
				writeAPIErrorResponse(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
				return
			}

			if !isTMAUserAllowed(r.Context(), st, user.ID) {
				appendTMAAudit(r.Context(), st, logger, store.AuditLog{
					Action:    "tma.auth.denied",
					ActorType: "telegram/tma",
					ActorID:   strconv.FormatInt(user.ID, 10),
					MetadataJSON: auditMetadataJSON(map[string]any{
						"reason":     "whitelist_rejected",
						"error_code": errorCodeForbidden,
						"request_id": reqID,
						"remote_ip":  remIP,
					}),
				})
				writeAPIErrorResponse(w, r, http.StatusForbidden, errorCodeForbidden, nil)
				return
			}

			ctx := ContextWithTelegramUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (s *server) tmaAuthMiddleware(next http.Handler) http.Handler {
	var kr cryptox.KeyRing
	if s.dependencies.KeyRing != nil {
		kr = *s.dependencies.KeyRing
	}
	return TMAAuthMiddleware(
		s.dependencies.Store,
		kr,
		s.dependencies.Config.Notify.Telegram.Token.Reveal(),
		s.dependencies.Logger,
	)(next)
}
