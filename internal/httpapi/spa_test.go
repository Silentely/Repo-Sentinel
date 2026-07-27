package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPA静态资源返回正确类型与不可变缓存(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"index.html":           &fstest.MapFile{Data: []byte("<!doctype html><title>fallback</title>")},
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
		},
	})

	response := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44001", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("静态资源状态=%d，响应=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/javascript") {
		t.Fatalf("静态资源 Content-Type=%q，期望 text/javascript", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("静态资源 Cache-Control=%q，期望不可变缓存", cacheControl)
	}
	if body := response.Body.String(); body != "console.log('ok');" {
		t.Fatalf("静态资源 body=%q，期望原始内容", body)
	}
}

func TestSPA浏览器路由返回index且不缓存(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>fallback</title>")},
		},
	})

	for _, route := range []string{"/login", "/repositories/42"} {
		response := fixture.request(t, http.MethodGet, route, "", "127.0.0.1:44002", nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("浏览器路由 %s 状态=%d，响应=%s", route, response.Code, response.Body.String())
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("浏览器路由 %s Content-Type=%q，期望 text/html", route, contentType)
		}
		if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-cache" {
			t.Fatalf("浏览器路由 %s Cache-Control=%q，期望 no-cache", route, cacheControl)
		}
	}
}

func TestSPA保留API与健康检查JSON404(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>fallback</title>")},
		},
	})

	for _, route := range []string{"/api", "/api/v1/missing", "/health", "/health/missing"} {
		response := fixture.request(t, http.MethodGet, route, "", "127.0.0.1:44003", nil, nil)
		assertAPIError(t, response, http.StatusNotFound, "not_found")
		if strings.Contains(response.Body.String(), "<!doctype") {
			t.Fatalf("保留路径 %s 被 SPA fallback 吞掉: %s", route, response.Body.String())
		}
	}
}

func TestSPA拒绝路径穿越与不存在静态资源(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"index.html":           &fstest.MapFile{Data: []byte("<!doctype html><title>fallback</title>")},
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
		},
	})

	for _, route := range []string{"/%2e%2e/secret.txt", "/assets", "/assets/%2e%2e/index.html", "/assets/missing.js"} {
		response := fixture.request(t, http.MethodGet, route, "", "127.0.0.1:44004", nil, nil)
		assertAPIError(t, response, http.StatusNotFound, "not_found")
	}
}

func TestSPA非GET请求不会回退到HTML(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>fallback</title>")},
		},
	})

	response := fixture.request(t, http.MethodPost, "/login", `{}`, "127.0.0.1:44005", nil, nil)
	assertAPIError(t, response, http.StatusNotFound, "not_found")
}
