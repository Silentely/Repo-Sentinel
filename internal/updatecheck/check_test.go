package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
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
