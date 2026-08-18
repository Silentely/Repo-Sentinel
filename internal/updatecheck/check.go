// Package updatecheck 提供可选的 GitHub Releases 远程版本检查。
// 行为对齐 TG-SignPulse dev：优先 HTML releases/latest 302 解析 tag（不吃 API 配额），
// 失败再回退 JSON/API；仅缓存成功结果；失败 soft-fail。
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	DefaultHTMLLatestURL = "https://github.com/Silentely/Repo-Sentinel/releases/latest"
	DefaultAPILatestURL  = "https://api.github.com/repos/Silentely/Repo-Sentinel/releases/latest"
	DefaultCacheTTL      = 6 * time.Hour
	defaultHTTPTimeout   = 8 * time.Second
	userAgent            = "RepoSentinel-VersionCheck/1.0"
)

var githubAPIReleasesRE = regexp.MustCompile(`(?i)^https://api\.github\.com/repos/([^/]+)/([^/]+)/releases/latest/?$`)

// Result 远程检查结果（失败 soft-fail，不抛给调用方业务错误）。
type Result struct {
	Enabled         bool   `json:"enabled"`
	LatestVersion   string `json:"latest_version,omitempty"`
	LatestURL       string `json:"latest_url,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       string `json:"checked_at"`
	Error           string `json:"error,omitempty"`
	Source          string `json:"source"`
	Cached          bool   `json:"cached"`
}

// Checker 可复用的版本检查器（进程内缓存）。
type Checker struct {
	Enabled    bool
	CheckURL   string // 默认 API latest；可自定义 JSON 源
	HTMLURL    string // 可选覆盖 HTML latest
	Token      string // 仅 JSON/API 路径可选
	Current    string // 本地版本
	HTTPClient *http.Client
	CacheTTL   time.Duration
	Now        func() time.Time
	// Logger 可选；检查成败 Debug 留痕（默认不输出）。
	Logger *slog.Logger

	mu    sync.Mutex
	cache *cachedPayload
}

type cachedPayload struct {
	expiresAt time.Time
	result    Result
}

// Check 执行远程检查；force 时跳过未过期缓存，失败仍可复用过期成功缓存。
// 各路径 Debug 留痕：缓存命中、成功（含来源与版本）、回退过期缓存、最终失败。
func (c *Checker) Check(ctx context.Context, force bool) Result {
	now := c.now()
	checkedAt := now.UTC().Format(time.RFC3339)
	if !c.Enabled {
		return Result{
			Enabled:   false,
			CheckedAt: checkedAt,
			Source:    "disabled",
		}
	}
	if !force {
		if hit := c.getCache(false); hit != nil {
			c.logf("update check cache hit", "cached", true)
			return *hit
		}
	}

	apiURL := strings.TrimSpace(c.CheckURL)
	if apiURL == "" {
		apiURL = DefaultAPILatestURL
	}
	htmlURL := strings.TrimSpace(c.HTMLURL)
	if htmlURL == "" {
		htmlURL = htmlLatestFromAPI(apiURL)
		if htmlURL == "" {
			htmlURL = DefaultHTMLLatestURL
		}
	}

	var lastErr error
	if htmlURL != "" {
		res, err := c.fetchViaHTMLRedirect(ctx, htmlURL)
		if err == nil {
			res.CheckedAt = checkedAt
			res.UpdateAvailable = IsUpdateAvailable(c.Current, res.LatestVersion)
			c.putSuccessCache(res)
			c.logf("update check ok", "version", res.LatestVersion, "source", res.Source, "cached", false)
			return res
		}
		lastErr = err
	}

	res, err := c.fetchJSONRelease(ctx, apiURL)
	if err == nil {
		res.CheckedAt = checkedAt
		res.UpdateAvailable = IsUpdateAvailable(c.Current, res.LatestVersion)
		c.putSuccessCache(res)
		c.logf("update check ok", "version", res.LatestVersion, "source", res.Source, "cached", false)
		return res
	}
	if lastErr == nil {
		lastErr = err
	}

	if stale := c.getCache(true); stale != nil {
		stale.Source = strings.TrimSuffix(stale.Source, "_stale") + "_stale"
		stale.Cached = true
		c.logf("update check stale", "version", stale.LatestVersion, "source", stale.Source)
		return *stale
	}
	c.logf("update check failed", "error", lastErr.Error(), "user_message", friendlyError(lastErr))
	return Result{
		Enabled:   true,
		CheckedAt: checkedAt,
		Error:     friendlyError(lastErr),
		Source:    "github_releases",
	}
}

// logf Debug 级输出检查路径；Logger 未注入时静默。
func (c *Checker) logf(msg string, attrs ...any) {
	if c.Logger != nil {
		c.Logger.Debug(msg, attrs...)
	}
}

// client 返回 HTTP 客户端；注入 HTTPClient 时以其为准。
// noFollow 为真时强制就地读取 Location（HTML 主路径自行解析跳转）。
func (c *Checker) client(noFollow bool) *http.Client {
	if c.HTTPClient != nil {
		cl := *c.HTTPClient
		if noFollow {
			cl.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
		return &cl
	}
	cl := &http.Client{Timeout: defaultHTTPTimeout}
	if noFollow {
		cl.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return cl
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) ttl() time.Duration {
	if c.CacheTTL > 0 {
		return c.CacheTTL
	}
	return DefaultCacheTTL
}

func (c *Checker) getCache(allowStale bool) *Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil || c.cache.result.LatestVersion == "" || c.cache.result.Error != "" {
		return nil
	}
	now := c.now()
	if !allowStale && now.After(c.cache.expiresAt) {
		return nil
	}
	out := c.cache.result
	out.Cached = true
	out.UpdateAvailable = IsUpdateAvailable(c.Current, out.LatestVersion)
	if allowStale && now.After(c.cache.expiresAt) {
		out.Source = strings.TrimSuffix(out.Source, "_stale") + "_stale"
	}
	return &out
}

func (c *Checker) putSuccessCache(res Result) {
	if res.LatestVersion == "" || res.Error != "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = &cachedPayload{
		expiresAt: c.now().Add(c.ttl()),
		result:    res,
	}
}

func (c *Checker) fetchViaHTMLRedirect(ctx context.Context, htmlLatest string) (Result, error) {
	safe, err := ValidateHTTPSURL(htmlLatest)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, safe, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", userAgent)

	// 不跟随跳转，只读 Location（与 TG-SignPulse 一致）。
	client := c.client(true)
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	location := resp.Header.Get("Location")
	finalURL := location
	tag := tagFromReleaseLocation(location)
	if tag == "" && location != "" {
		if u, e := resp.Request.URL.Parse(location); e == nil {
			finalURL = u.String()
			tag = tagFromReleaseLocation(finalURL)
		}
	}
	if tag == "" && resp.StatusCode == http.StatusOK {
		tag = tagFromReleaseLocation(resp.Request.URL.String())
		finalURL = resp.Request.URL.String()
	}
	if tag == "" {
		// 再试跟随跳转
		follow := c.client(false)
		resp2, err := follow.Do(req.Clone(ctx))
		if err != nil {
			return Result{}, fmt.Errorf("无法从 releases/latest 解析 tag: %w", err)
		}
		defer resp2.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp2.Body, 64<<10))
		tag = tagFromReleaseLocation(resp2.Request.URL.String())
		finalURL = resp2.Request.URL.String()
		if tag == "" {
			return Result{}, fmt.Errorf("无法从 releases/latest 解析 tag（HTTP %d）", resp.StatusCode)
		}
	}
	page := SafeReleasePageURL(finalURL)
	if page == "" {
		page = safe
	}
	return Result{
		Enabled:       true,
		LatestVersion: NormalizeVersion(tag),
		LatestURL:     page,
		Source:        "github_releases_redirect",
	}, nil
}

func (c *Checker) fetchJSONRelease(ctx context.Context, rawURL string) (Result, error) {
	safe, err := ValidateHTTPSURL(rawURL)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, safe, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if tok := strings.TrimSpace(c.Token); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := c.client(false)
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return Result{}, fmt.Errorf("rate limit or forbidden: HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("远程版本源返回 HTTP %d", resp.StatusCode)
	}
	var data struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Result{}, fmt.Errorf("release payload is not valid json: %w", err)
	}
	tag := strings.TrimSpace(data.TagName)
	if tag == "" {
		tag = strings.TrimSpace(data.Name)
	}
	if tag == "" {
		return Result{}, fmt.Errorf("release missing tag_name")
	}
	source := "custom_release_json"
	if strings.Contains(safe, "api.github.com") {
		source = "github_releases"
	}
	page := SafeReleasePageURL(data.HTMLURL)
	return Result{
		Enabled:       true,
		LatestVersion: NormalizeVersion(tag),
		LatestURL:     page,
		Source:        source,
	}, nil
}

// ValidateHTTPSURL 仅允许 https，降低 SSRF/误配风险。
func ValidateHTTPSURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("update check URL is empty")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("update check URL is invalid")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("update check URL must use https")
	}
	if u.Host == "" || u.User != nil {
		return "", fmt.Errorf("update check URL host is invalid")
	}
	return s, nil
}

// SafeReleasePageURL 仅保留 http(s) 发布页链接。
func SafeReleasePageURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if u.Host == "" {
		return ""
	}
	return s
}

func htmlLatestFromAPI(apiURL string) string {
	m := githubAPIReleasesRE.FindStringSubmatch(strings.TrimSpace(apiURL))
	if len(m) != 3 {
		if strings.TrimSpace(apiURL) == "" || strings.TrimSpace(apiURL) == DefaultAPILatestURL {
			return DefaultHTMLLatestURL
		}
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest", m[1], m[2])
}

func tagFromReleaseLocation(location string) string {
	raw := strings.TrimSpace(location)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	const marker = "/releases/tag/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return ""
	}
	tag := strings.Trim(u.Path[idx+len(marker):], "/")
	if i := strings.IndexByte(tag, '/'); i >= 0 {
		tag = tag[:i]
	}
	return strings.TrimSpace(tag)
}

func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "http 403") || strings.Contains(lower, "http 429") {
		return "检查更新暂时失败，请稍后再试。"
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
		return "连接版本源超时，请检查网络后重试。"
	}
	// 未分类错误不向客户端透出原始文本（可能含 TLS/DNS/代理等内部细节），
	// 统一为可操作的通用文案；完整错误由调用方日志保留。
	return "无法获取最新版本，请稍后重试。"
}
