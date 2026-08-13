package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
)

const (
	// oauthSigningAAD 是派生 OAuth 签名密钥的关联数据，变更即作废全部已签发令牌。
	oauthSigningAAD = "oauth:signing:v1"
	// oauthTokenTTL 是访问令牌有效期，Agent 凭据为长期客户端，1 小时足够轮换。
	oauthTokenTTL = time.Hour
	// oauthAPIScope 是当前唯一支持的作用域：只读访问管理 API。
	oauthAPIScope = "read"
)

var (
	errOAuthUnavailable  = errors.New("oauth_unavailable")
	errOAuthInvalidToken = errors.New("invalid_token")
)

// oauthClaims 是签发的访问令牌负载。
type oauthClaims struct {
	Scope string `json:"scope"`
	jwt.RegisteredClaims
}

// oauthSigningKey 从主密钥派生稳定的 HS256 签名密钥（32 字节）。
// KeyRing 不可用时返回 errOAuthUnavailable，token/jwks 端点随即 503。
func (s *server) oauthSigningKey() ([]byte, error) {
	if s.dependencies.KeyRing == nil {
		return nil, errOAuthUnavailable
	}
	derived, err := s.dependencies.KeyRing.DeriveHMACKey([]byte(oauthSigningAAD))
	if err != nil {
		return nil, errOAuthUnavailable
	}
	sum := sha256.Sum256(derived)
	return sum[:], nil
}

// oauthKeyID 返回签名密钥的短标识，写入 JWT kid 与 JWKS，便于轮换后客户端缓存失效。
func oauthKeyID(key []byte) string {
	sum := sha256.Sum256(key)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

// oauthConfigured 判定是否已配置 Agent 客户端凭据。
func (s *server) oauthConfigured() bool {
	return strings.TrimSpace(s.dependencies.Config.OAuth.ClientSecret.Reveal()) != ""
}

// oauthClientCredentials 从请求提取客户端凭据：优先 Basic Auth，其次表单字段（RFC 6749 2.3.1）。
func oauthClientCredentials(r *http.Request) (clientID, clientSecret string, ok bool) {
	if id, secret, found := r.BasicAuth(); found {
		return id, secret, true
	}
	clientID = r.Form.Get("client_id")
	clientSecret = r.Form.Get("client_secret")
	return clientID, clientSecret, clientID != "" && clientSecret != ""
}

// writeOAuthError 按 RFC 6749 5.2 输出错误响应。
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="reposentinel-oauth", error="invalid_client"`)
	}
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

// handleOAuthToken 实现 OAuth 2.0 client_credentials 令牌签发。
func (s *server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "请求体不是合法的 application/x-www-form-urlencoded。")
		return
	}
	if r.Form.Get("grant_type") != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "本实例仅支持 client_credentials。")
		return
	}
	clientID, clientSecret, ok := oauthClientCredentials(r)
	if !ok {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "缺少客户端凭据，请携带 client_id/client_secret 或 Basic Auth。")
		return
	}
	// 令牌端点复用登录限流器：client_secret 是长期静态凭据，按来源 IP 节流
	// 防止无限暴力尝试（每次失败都会刷 Warn 日志）。
	remoteIP := remoteIPFromContext(r.Context())
	if !s.dependencies.LoginLimiter.Allow(remoteIP) {
		s.dependencies.Logger.Warn(
			"oauth token rate limited",
			"request_id", requestIDFromContext(r.Context()),
			"remote_ip", remoteIP,
			"error_code", errorCodeRateLimited,
		)
		w.Header().Set("Retry-After", loginRetryAfterSeconds)
		writeOAuthError(w, http.StatusTooManyRequests, "invalid_client", "尝试过于频繁，请稍后再试。")
		return
	}
	configuredID := s.dependencies.Config.OAuth.ClientID
	configuredSecret := s.dependencies.Config.OAuth.ClientSecret.Reveal()
	secretMatch := configuredSecret != "" &&
		subtle.ConstantTimeCompare([]byte(clientSecret), []byte(configuredSecret)) == 1
	if configuredID == "" || !strings.EqualFold(clientID, configuredID) || !secretMatch {
		s.dependencies.Logger.Warn(
			"oauth token rejected",
			"request_id", requestIDFromContext(r.Context()),
			"remote_ip", remoteIPFromContext(r.Context()),
			"error_code", "invalid_client",
		)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "客户端凭据无效。")
		return
	}
	key, err := s.oauthSigningKey()
	if err != nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	origin := s.siteOrigin(r)
	now := time.Now()
	claims := oauthClaims{
		Scope: oauthAPIScope,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    origin,
			Subject:   clientID,
			Audience:  jwt.ClaimStrings{origin + "/api/v1"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(oauthTokenTTL)),
			ID:        ulid.Make().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = oauthKeyID(key)
	signed, err := token.SignedString(key)
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": signed,
		"token_type":   "Bearer",
		"expires_in":   int(oauthTokenTTL.Seconds()),
		"scope":        oauthAPIScope,
	})
}

// handleOAuthAuthorize 是 metadata 声明的授权端点；本实例不支持交互式授权流。
func (s *server) handleOAuthAuthorize(w http.ResponseWriter, _ *http.Request) {
	writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "本实例仅支持 client_credentials 授权流。")
}

// handleOAuthJWKS 暴露签名公钥（oct 类型），供客户端按 kid 校验令牌签名。
func (s *server) handleOAuthJWKS(w http.ResponseWriter, r *http.Request) {
	key, err := s.oauthSigningKey()
	if err != nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "oct",
				"alg": "HS256",
				"use": "sig",
				"kid": oauthKeyID(key),
				"k":   base64.RawURLEncoding.EncodeToString(key),
			},
		},
	})
}

// oauthValidateToken 校验 Bearer 令牌签名与声明，返回客户端标识。
// expectedAudience 通常为站点 Origin + "/api/v1"；expectedIssuer 为站点 Origin。
func (s *server) oauthValidateToken(tokenString, expectedAudience, expectedIssuer string) (string, error) {
	key, err := s.oauthSigningKey()
	if err != nil {
		return "", err
	}
	parsed, err := jwt.ParseWithClaims(
		tokenString,
		&oauthClaims{},
		func(_ *jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(expectedAudience),
		jwt.WithIssuer(expectedIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return "", errOAuthInvalidToken
	}
	claims, ok := parsed.Claims.(*oauthClaims)
	if !ok || claims.Subject == "" {
		return "", errOAuthInvalidToken
	}
	return claims.Subject, nil
}

// bearerToken 提取 Authorization: Bearer <token>（scheme 大小写不敏感，RFC 7235 允许 bearer）。
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	// 取首个空白分隔段并比较 scheme；避免 strings.HasPrefix 的大小写敏感误拒。
	space := strings.IndexByte(header, ' ')
	if space <= 0 || !strings.EqualFold(header[:space], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(header[space+1:])
	return token, token != ""
}

// agentClientContextKey 携带 OAuth Bearer 认证通过的客户端标识。
const agentClientContextKey contextKey = "agent_client_id"

func agentClientIDFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(agentClientContextKey).(string)
	return value, ok && value != ""
}
