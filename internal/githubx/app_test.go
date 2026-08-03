package githubx

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAppJWTAndConfigured(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewAppClient(12345, path)
	if !c.Configured() {
		t.Fatal("should be configured")
	}
	token, err := c.AppJWT()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("jwt invalid: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims type")
	}
	if claims["iss"] != "12345" {
		t.Fatalf("iss=%v", claims["iss"])
	}
	exp := int64(claims["exp"].(float64))
	if time.Until(time.Unix(exp, 0)) > 10*time.Minute || time.Until(time.Unix(exp, 0)) < time.Minute {
		t.Fatalf("unexpected exp window: %v", time.Until(time.Unix(exp, 0)))
	}
}

func TestAppClientNotConfigured(t *testing.T) {
	c := NewAppClient(0, "")
	if c.Configured() {
		t.Fatal("empty should not be configured")
	}
	if _, err := c.AppJWT(); err == nil {
		t.Fatal("expected error")
	}
}

// TestDoJSONErrorClassification 验证 doJSON 的错误分类与 Link header 传递：
// 403 限流（X-RateLimit-Remaining=0 或 body 含 rate limit）与 429 → github_rate_limited；
// 403 权限/功能错误（如 Advanced Security 未启用）必须保留 HTTPStatusError，不能误判为限流；
// 200 响应经 DoJSONPage 能取回 Link header 供游标分页使用。
func TestDoJSONErrorClassification(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rate-limited-header", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	})
	mux.HandleFunc("/rate-limited-body", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
	})
	mux.HandleFunc("/forbidden", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Advanced Security must be enabled"}`))
	})
	mux.HandleFunc("/too-many", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/ok-link", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/repos/a/b/dependabot/alerts?after=cursor2&per_page=50>; rel="next"`)
		_, _ = w.Write([]byte(`[{"number":1}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &AppClient{HTTP: srv.Client(), BaseURL: srv.URL}

	for _, tc := range []struct {
		name, path, wantErr string
	}{
		{"403 主限流", "/rate-limited-header", "github_rate_limited"},
		{"403 次限流", "/rate-limited-body", "github_rate_limited"},
		{"429 限流", "/too-many", "github_rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.DoJSON(context.Background(), http.MethodGet, tc.path, "tok", nil); err == nil || err.Error() != tc.wantErr {
				t.Fatalf("got %v, want %s", err, tc.wantErr)
			}
		})
	}

	// 403 权限/功能错误必须保留 HTTPStatusError（否则会被下游误判为限流并错误重试）。
	_, err := c.DoJSON(context.Background(), http.MethodGet, "/forbidden", "tok", nil)
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		t.Fatalf("403 功能错误应为 HTTPStatusError{403}, got %v", err)
	}

	// DoJSONPage 应把 Link header 交还调用方（dependabot 游标分页依赖）。
	var out []AlertItem
	_, link, err := c.DoJSONPage(context.Background(), http.MethodGet, "/ok-link", "tok", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "after=cursor2") {
		t.Fatalf("Link header 未返回, got %q", link)
	}
}

// TestParseNextAfter 验证从 Link header 提取 rel="next" 的 after 游标。
func TestParseNextAfter(t *testing.T) {
	link := `<https://api.github.com/repos/a/b/dependabot/alerts?after=abc123&per_page=50>; rel="next", <https://api.github.com/repos/a/b/dependabot/alerts?before=zzz&per_page=50>; rel="prev"`
	if got := parseNextAfter(link); got != "abc123" {
		t.Fatalf("parseNextAfter = %q, want abc123", got)
	}
	if got := parseNextAfter(""); got != "" {
		t.Fatalf("空 Link 应返回空游标, got %q", got)
	}
	onlyPrev := `<https://api.github.com/repos/a/b/dependabot/alerts?before=zzz>; rel="prev"`
	if got := parseNextAfter(onlyPrev); got != "" {
		t.Fatalf("仅 prev 应返回空游标, got %q", got)
	}
}

func TestAppClient内存PEM与Configure热更新(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

	c := NewAppClient(0, "")
	if c.Configured() {
		t.Fatal("empty should not be configured")
	}
	c.Configure(99, "", string(pemBytes))
	if !c.Configured() {
		t.Fatal("pem should configure client")
	}
	if !c.HasPrivateKeyMaterial() {
		t.Fatal("expected private key material")
	}
	token, err := c.AppJWT()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("jwt invalid: %v", err)
	}
	if err := ValidatePrivateKeyPEM(string(pemBytes)); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateKeyPEM("not-a-key"); err == nil {
		t.Fatal("expected invalid pem error")
	}
}

// TestParseRetryAfterHeader 验证 Retry-After 响应头解析：整数秒 / HTTP 日期 / 非法 / 过期。
func TestParseRetryAfterHeader(t *testing.T) {
	future := time.Now().UTC().Add(90 * time.Second)
	cases := []struct {
		name string
		head string
		want time.Duration
	}{
		{"空头", "", 0},
		{"整数秒", "60", 60 * time.Second},
		{"最小有效秒", "1", time.Second},
		{"空白填充整数", "  60  ", 60 * time.Second},
		{"零秒不采纳", "0", 0},
		{"负秒不采纳", "-5", 0},
		{"非整数数字", "1.5", 0},
		{"非法文本", "abc", 0},
		// 超大整数无封顶：该解析器按上游原始时长返回，由调用方自行约束等待预算。
		{"超大整数按原值返回", "99999999", 99999999 * time.Second},
		{"HTTP 日期", future.Format(http.TimeFormat), 90 * time.Second},
		{"空白填充日期", "  " + future.Format(http.TimeFormat) + " ", 90 * time.Second},
		{"已过期日期", time.Now().UTC().Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfterHeader(tc.head)
			// HTTP 日期按剩余秒数计算，允许少量执行偏差。
			if tc.want == 0 {
				if got != 0 {
					t.Fatalf("期望 0，实际 %v", got)
				}
				return
			}
			if got < tc.want-2*time.Second || got > tc.want+2*time.Second {
				t.Fatalf("期望约 %v，实际 %v", tc.want, got)
			}
		})
	}
}

// TestResetDelta 验证 X-RateLimit-Reset（unix 秒）换算为剩余等待时长。
func TestResetDelta(t *testing.T) {
	if got := resetDelta(""); got != 0 {
		t.Fatalf("空头应返回 0，实际 %v", got)
	}
	if got := resetDelta("abc"); got != 0 {
		t.Fatalf("非法头应返回 0，实际 %v", got)
	}
	if got := resetDelta(strconv.FormatInt(time.Now().Unix()-60, 10)); got != 0 {
		t.Fatalf("已过期 Reset 应返回 0，实际 %v", got)
	}
	delta := time.Until(time.Unix(time.Now().Unix()+300, 0))
	if got := resetDelta(strconv.FormatInt(time.Now().Unix()+300, 10)); got < delta-2*time.Second || got > delta+2*time.Second {
		t.Fatalf("期望约 %v，实际 %v", delta, got)
	}
}

// TestDoJSONCarriesRetryAfter 验证限流错误携带上游建议的等待时长：
// 429 Retry-After（秒或 HTTP 日期）、403 配额耗尽的 X-RateLimit-Reset 换算；
// 非限流 403 仍为 HTTPStatusError。
func TestDoJSONCarriesRetryAfter(t *testing.T) {
	future := time.Now().UTC().Add(90 * time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/429-secs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/429-date", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", future.Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/429-nohdr", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/403-reset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Unix()+300, 10))
		w.WriteHeader(http.StatusForbidden)
	})
	mux.HandleFunc("/403-perm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Advanced Security must be enabled"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &AppClient{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	_, err := c.DoJSON(ctx, http.MethodGet, "/429-secs", "tok", nil)
	if !IsRateLimited(err) || RetryAfterOf(err) != 60*time.Second {
		t.Fatalf("429 秒数应携带 60s，got err=%v retry=%v", err, RetryAfterOf(err))
	}
	_, err = c.DoJSON(ctx, http.MethodGet, "/429-date", "tok", nil)
	if ra := RetryAfterOf(err); !IsRateLimited(err) || ra < 88*time.Second || ra > 92*time.Second {
		t.Fatalf("429 HTTP 日期应携带约 90s，got retry=%v", ra)
	}
	_, err = c.DoJSON(ctx, http.MethodGet, "/429-nohdr", "tok", nil)
	if !IsRateLimited(err) || RetryAfterOf(err) != 0 {
		t.Fatalf("429 无响应头应携带 0，got err=%v retry=%v", err, RetryAfterOf(err))
	}
	_, err = c.DoJSON(ctx, http.MethodGet, "/403-reset", "tok", nil)
	if ra := RetryAfterOf(err); !IsRateLimited(err) || ra < 298*time.Second || ra > 302*time.Second {
		t.Fatalf("403 配额耗尽应按 Reset 换算约 300s，got retry=%v", ra)
	}
	_, err = c.DoJSON(ctx, http.MethodGet, "/403-perm", "tok", nil)
	if IsRateLimited(err) {
		t.Fatalf("403 权限错误不应判为限流: %v", err)
	}
	if _, ok := err.(*HTTPStatusError); !ok {
		t.Fatalf("403 权限错误应为 HTTPStatusError，got %T %v", err, err)
	}
}

// TestInstallationTokenRateLimited 验证 token 签发端点 429 同样归类为限流错误并携带 Retry-After。
func TestInstallationTokenRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewAppClient(12345, "")
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL
	// 绕过 Configured 前置校验：注入私钥材料满足检查。
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	c.Configure(12345, "", string(pemBytes))

	_, err = c.InstallationToken(context.Background(), 4242)
	if !IsRateLimited(err) || RetryAfterOf(err) != 30*time.Second {
		t.Fatalf("token 429 应携带 30s，got err=%v retry=%v", err, RetryAfterOf(err))
	}
}

// 并发获取同一 installation 的 token 应合并为一次签发请求（single-flight），
// 其余调用者共享结果；缓存命中后不再请求签发端点。
func TestInstallationTokenConcurrentSingleFlight(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"tok-123","expires_at":%q}`, expires)
	}))
	defer srv.Close()

	c := NewAppClient(12345, "")
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL
	// 注入私钥材料以满足 Configured 前置校验。
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	c.Configure(12345, "", string(pemBytes))

	const n = 10
	start := make(chan struct{})
	results := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = c.InstallationToken(context.Background(), 4242)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if results[i] != "tok-123" {
			t.Fatalf("goroutine %d: 期望 tok-123，实际 %q", i, results[i])
		}
	}
	mu.Lock()
	got := requests
	mu.Unlock()
	if got != 1 {
		t.Fatalf("并发获取应合并为 1 次签发请求，实际 %d", got)
	}

	// 缓存命中：再次调用不应新增签发请求。
	if _, err := c.InstallationToken(context.Background(), 4242); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got = requests
	mu.Unlock()
	if got != 1 {
		t.Fatalf("缓存命中不应新增签发请求，实际 %d", got)
	}
}
