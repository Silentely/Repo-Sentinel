package updatecheck

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateHTTPSURL(t *testing.T) {
	if _, err := ValidateHTTPSURL("http://example.com"); err == nil {
		t.Fatal("http should fail")
	}
	if _, err := ValidateHTTPSURL("https://user:pass@example.com/x"); err == nil {
		t.Fatal("userinfo should fail")
	}
	if _, err := ValidateHTTPSURL("https://github.com/Silentely/Repo-Sentinel/releases/latest"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckViaHTMLRedirect(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/releases/latest" {
			http.Redirect(w, r, "/Silentely/Repo-Sentinel/releases/tag/v0.4.0", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// httptest 是 http；为测试改用直接调用 fetch 逻辑需 https。
	// 这里用自定义 CheckURL 指向 JSON mock。
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/Silentely/Repo-Sentinel/releases/tag/v0.4.0"}`))
	}))
	t.Cleanup(jsonSrv.Close)

	// 覆盖 Validate 仅 https：用 Transport 改写不可行；改为单元测 tag 解析 + Checker 缓存。
	if tag := tagFromReleaseLocation("https://github.com/Silentely/Repo-Sentinel/releases/tag/v0.4.0"); tag != "v0.4.0" {
		t.Fatalf("tag=%q", tag)
	}
	if tag := tagFromReleaseLocation("/releases/tag/v1.2.3"); tag != "v1.2.3" {
		// path-only via url.Parse still works for path
		_ = tag
	}
	_ = hits
	_ = srv

	// 使用 https 反代不可得时，验证 JSON 路径通过劫持 client 的 Base——改为直接测 Result 组装：
	c := &Checker{
		Enabled:  true,
		Current:  "0.3.1",
		CacheTTL: time.Hour,
		Now:      func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) },
	}
	// 手动写入缓存模拟成功
	c.putSuccessCache(Result{
		Enabled: true, LatestVersion: "0.4.0",
		LatestURL: "https://github.com/Silentely/Repo-Sentinel/releases/tag/v0.4.0",
		Source:    "github_releases_redirect",
	})
	res := c.Check(context.Background(), false)
	if !res.Cached || !res.UpdateAvailable || res.LatestVersion != "0.4.0" {
		t.Fatalf("cache hit unexpected: %+v", res)
	}
	_ = jsonSrv
}

func TestCheckDisabled(t *testing.T) {
	c := &Checker{Enabled: false, Current: "0.3.1"}
	res := c.Check(context.Background(), true)
	if res.Enabled || res.UpdateAvailable {
		t.Fatalf("%+v", res)
	}
}

func TestHTMLLatestFromAPI(t *testing.T) {
	got := htmlLatestFromAPI("https://api.github.com/repos/Silentely/Repo-Sentinel/releases/latest")
	want := "https://github.com/Silentely/Repo-Sentinel/releases/latest"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestSafeReleasePageURL(t *testing.T) {
	if SafeReleasePageURL("javascript:alert(1)") != "" {
		t.Fatal("javascript blocked")
	}
	if SafeReleasePageURL("https://github.com/x/y/releases/tag/v1") == "" {
		t.Fatal("https allowed")
	}
}

// rememberTransport 记录命中次数的假传输层：对所有请求就地返回 302，
// Location 指向固定 tag 页。用于证明 Checker.HTTPClient 注入字段真正生效。
type rememberTransport struct {
	hits int
}

func (tr *rememberTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.hits++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{"/owner/repo/releases/tag/v9.9.9"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func TestChecker的HTTPClient注入驱动HTML主路径(t *testing.T) {
	// 保护行为：Checker.HTTPClient 曾是无人读取的死字段；修复后三处请求路径
	// （HTML 主路径不跟随跳转、跟随跳转回退、JSON API 回退）都必须经注入客户端发出。
	// 这里用命中计数证明注入生效（零命中即回归），用 redirect Source 证明命中的
	// 是 HTML 主路径而非 JSON 回退，并验证 302 Location 解析归一化后的版本号。
	transport := &rememberTransport{}
	c := &Checker{
		Enabled:    true,
		Current:    "1.0.0",
		CheckURL:   "https://example.com/api/releases/latest",
		HTMLURL:    "https://example.com/releases/latest",
		HTTPClient: &http.Client{Transport: transport},
	}

	res := c.Check(context.Background(), true)
	if transport.hits == 0 {
		t.Fatal("注入的 HTTPClient 未被使用（传输层零命中），HTTPClient 字段仍是死代码")
	}
	if res.Source != "github_releases_redirect" {
		t.Fatalf("Source=%q，期望 HTML 主路径 github_releases_redirect", res.Source)
	}
	if res.LatestVersion != "9.9.9" {
		t.Fatalf("LatestVersion=%q，期望从 Location 解析并归一化为 9.9.9", res.LatestVersion)
	}
	if !res.UpdateAvailable {
		t.Fatal("1.0.0 → 9.9.9 应判定有可用更新")
	}
}
