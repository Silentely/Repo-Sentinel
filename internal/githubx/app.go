package githubx

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppClient 使用 GitHub App 私钥签发 JWT，并缓存 Installation Token。
type AppClient struct {
	AppID          int64
	PrivateKeyPath string
	// PrivateKeyPEM 可选：内存中的 PEM 明文（管理台写入）；优先于路径文件。
	PrivateKeyPEM string
	HTTP          *http.Client
	BaseURL       string // 默认 https://api.github.com

	mu    sync.Mutex
	key   *rsa.PrivateKey
	cache map[int64]cachedToken
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// NewAppClient 创建客户端；AppID 或私钥为空时仍可构造，调用时再报错。
func NewAppClient(appID int64, privateKeyPath string) *AppClient {
	return &AppClient{
		AppID:          appID,
		PrivateKeyPath: privateKeyPath,
		HTTP:           &http.Client{Timeout: 30 * time.Second},
		BaseURL:        "https://api.github.com",
		cache:          make(map[int64]cachedToken),
	}
}

// Configure 热更新 App 身份与私钥来源，并清空缓存密钥与 Installation Token。
func (c *AppClient) Configure(appID int64, privateKeyPath, privateKeyPEM string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AppID = appID
	c.PrivateKeyPath = strings.TrimSpace(privateKeyPath)
	c.PrivateKeyPEM = strings.TrimSpace(privateKeyPEM)
	c.key = nil
	c.cache = make(map[int64]cachedToken)
}

// Configured 表示具备 App 调用条件（App ID + 路径或内存 PEM 其一）。
func (c *AppClient) Configured() bool {
	if c == nil || c.AppID <= 0 {
		return false
	}
	return strings.TrimSpace(c.PrivateKeyPath) != "" || strings.TrimSpace(c.PrivateKeyPEM) != ""
}

// HasPrivateKeyMaterial 表示已配置路径或 PEM（不验证文件是否可读）。
func (c *AppClient) HasPrivateKeyMaterial() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.PrivateKeyPEM) != "" {
		return true
	}
	path := strings.TrimSpace(c.PrivateKeyPath)
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (c *AppClient) loadKey() (*rsa.PrivateKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != nil {
		return c.key, nil
	}
	var raw []byte
	var err error
	if pemText := strings.TrimSpace(c.PrivateKeyPEM); pemText != "" {
		raw = []byte(pemText)
	} else {
		path := strings.TrimSpace(c.PrivateKeyPath)
		if path == "" {
			return nil, fmt.Errorf("missing private key")
		}
		raw, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
	}
	key, err := parseRSAPrivateKeyPEM(raw)
	if err != nil {
		return nil, err
	}
	c.key = key
	return key, nil
}

func parseRSAPrivateKeyPEM(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("invalid pem private key")
	}
	var key *rsa.PrivateKey
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, e
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not rsa private key")
		}
	default:
		return nil, fmt.Errorf("unsupported key type %s", block.Type)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

// ValidatePrivateKeyPEM 校验 PEM 是否为可用的 RSA 私钥（不落盘）。
func ValidatePrivateKeyPEM(pemText string) error {
	_, err := parseRSAPrivateKeyPEM([]byte(strings.TrimSpace(pemText)))
	return err
}

// AppJWT 签发短时 App JWT（约 9 分钟）。
func (c *AppClient) AppJWT() (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("github_app_not_configured")
	}
	key, err := c.loadKey()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Add(-30 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(c.AppID, 10),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// InstallationToken 获取并缓存 installation access token。
func (c *AppClient) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("github_app_not_configured")
	}
	c.mu.Lock()
	if tok, ok := c.cache[installationID]; ok && time.Now().Before(tok.ExpiresAt.Add(-2*time.Minute)) {
		c.mu.Unlock()
		return tok.Token, nil
	}
	c.mu.Unlock()

	jwtToken, err := c.AppJWT()
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", base, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		// 429：次限流，按 Retry-After 响应头给出等待时长。
		return "", &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode == http.StatusForbidden {
		// 403 需区分限流与权限/配置错误：主限流响应携带 X-RateLimit-Remaining: 0。
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || bytes.Contains(body, []byte("rate limit")) {
			return "", &RateLimitError{RetryAfter: resetDelta(resp.Header.Get("X-RateLimit-Reset"))}
		}
		return "", fmt.Errorf("installation_token_http_%d", resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("installation_token_http_%d", resp.StatusCode)
	}
	var parsed struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	c.mu.Lock()
	// cache 仅在构造函数中初始化；防御结构体字面量构造下的 nil map 写 panic。
	if c.cache == nil {
		c.cache = make(map[int64]cachedToken)
	}
	c.cache[installationID] = cachedToken{Token: parsed.Token, ExpiresAt: parsed.ExpiresAt}
	c.mu.Unlock()
	return parsed.Token, nil
}

// DoJSON 使用 bearer token 请求 GitHub API。
func (c *AppClient) DoJSON(ctx context.Context, method, path, token string, out any) (rateRemaining int, err error) {
	rateRemaining, _, err = c.doJSON(ctx, method, path, token, out)
	return rateRemaining, err
}

// DoJSONPage 与 DoJSON 相同，额外返回响应的 Link header，供游标分页端点使用。
func (c *AppClient) DoJSONPage(ctx context.Context, method, path, token string, out any) (rateRemaining int, link string, err error) {
	return c.doJSON(ctx, method, path, token, out)
}

// doJSON 请求 GitHub API 并返回剩余配额与 Link header。
// 错误分类：429 与 403（X-RateLimit-Remaining=0 或 body 含 rate limit）→ github_rate_limited；
// 其余 403/4xx/5xx → HTTPStatusError（403 需区分限流与权限/功能未开启，不能一律当限流）。
func (c *AppClient) doJSON(ctx context.Context, method, path, token string, out any) (rateRemaining int, link string, err error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://api.github.com"
	}
	full := path
	if strings.HasPrefix(path, "/") {
		full = c.BaseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, full, nil)
	if err != nil {
		return 0, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		rateRemaining, _ = strconv.Atoi(v)
	}
	link = resp.Header.Get("Link")
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusNotModified {
		return rateRemaining, link, errNotModified
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// 429：次限流，按 Retry-After 响应头给出等待时长。
		return rateRemaining, link, &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode == http.StatusForbidden {
		// 403 需区分限流与权限/功能未开启：主限流响应携带 X-RateLimit-Remaining: 0。
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || bytes.Contains(body, []byte("rate limit")) {
			return rateRemaining, link, &RateLimitError{RetryAfter: resetDelta(resp.Header.Get("X-RateLimit-Reset"))}
		}
		return rateRemaining, link, statusError(resp.StatusCode, body)
	}
	if resp.StatusCode >= 300 {
		return rateRemaining, link, statusError(resp.StatusCode, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return rateRemaining, link, err
		}
	}
	return rateRemaining, link, nil
}

// statusError 构造 HTTPStatusError。
func statusError(status int, _ []byte) error {
	return &HTTPStatusError{StatusCode: status}
}

var errNotModified = fmt.Errorf("not_modified")

// IsNotModified 判断 304。
func IsNotModified(err error) bool {
	return err == errNotModified
}

// RateLimitError GitHub 限流错误：429 次限流或配额耗尽的 403。
// RetryAfter 为上游建议的等待时长（0 表示未知），调用方按自身预算决定等待或放弃本轮。
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "github_rate_limited" }

// IsRateLimited 判断错误是否为 GitHub 限流（429 或配额耗尽的 403）。
func IsRateLimited(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}

// RetryAfterOf 返回限流错误携带的建议等待时长；非限流错误返回 0。
func RetryAfterOf(err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	return 0
}

// parseRetryAfterHeader 解析 Retry-After 响应头：整数秒或 HTTP 日期。
// 头缺失、格式非法或日期已过期时返回 0。
func parseRetryAfterHeader(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		return d
	}
	return 0
}

// resetDelta 把 X-RateLimit-Reset（unix 秒）换算为剩余等待时长；缺失或已过期返回 0。
func resetDelta(header string) time.Duration {
	ts, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64)
	if err != nil {
		return 0
	}
	d := time.Until(time.Unix(ts, 0))
	if d <= 0 {
		return 0
	}
	return d
}

// HTTPStatusError 携带 GitHub API 的 HTTP 状态码，便于调用方按状态分类
// （如 404/403 判定功能未开启或仓库不可见，其余错误视为临时故障）。
type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("github_http_%d", e.StatusCode) }
