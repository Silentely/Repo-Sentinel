package githubx

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
