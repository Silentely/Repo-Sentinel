package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"
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

func TestSPA文本资源按gzip压缩且内容可解压(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte(strings.Repeat("console.log('ok');", 100))},
		},
	})

	response := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44006", nil, map[string]string{
		"Accept-Encoding": "gzip",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("状态=%d，响应=%s", response.Code, response.Body.String())
	}
	if enc := response.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding=%q，期望 gzip", enc)
	}
	if vary := response.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("Vary=%q 应包含 Accept-Encoding", vary)
	}
	if response.Header().Get("Content-Length") != "" {
		t.Fatal("gzip 响应不应携带 Content-Length（长度与原始字节不一致）")
	}
	gz, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("响应体不是合法 gzip: %v", err)
	}
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gzip 解压失败: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "console.log('ok');") {
		t.Fatalf("解压内容不符，前 40 字节: %q", decoded[:min(40, len(decoded))])
	}
}

func TestSPA不支持gzip时不压缩(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
		},
	})

	// 无 Accept-Encoding
	plain := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44007", nil, nil)
	if enc := plain.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("无 Accept-Encoding 不应压缩，got %q", enc)
	}
	if plain.Body.String() != "console.log('ok');" {
		t.Fatal("无 Accept-Encoding 应返回原始内容")
	}
	// gzip;q=0 显式拒绝
	refused := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44008", nil, map[string]string{
		"Accept-Encoding": "gzip;q=0",
	})
	if enc := refused.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("gzip;q=0 不应压缩，got %q", enc)
	}
}

func TestSPARange请求不gzip(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
		},
	})

	response := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44009", nil, map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-9",
	})
	if enc := response.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Range 请求不应 gzip，got %q", enc)
	}
	if code := response.Code; code != http.StatusPartialContent {
		t.Fatalf("Range 请求应返回 206，got %d", code)
	}
}

func TestSPAGzipNotModified不带编码头(t *testing.T) {
	modTime := time.Unix(1700000000, 0)
	fixture := newHTTPTestFixture(t, httpTestOptions{
		frontend: fstest.MapFS{
			"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok');"), ModTime: modTime},
		},
	})

	response := fixture.request(t, http.MethodGet, "/assets/app-abc123.js", "", "127.0.0.1:44010", nil, map[string]string{
		"Accept-Encoding":   "gzip",
		"If-Modified-Since": modTime.UTC().Format(http.TimeFormat),
	})
	if response.Code != http.StatusNotModified {
		t.Fatalf("命中缓存应返回 304，got %d", response.Code)
	}
	if enc := response.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("304 响应不应携带 Content-Encoding，got %q", enc)
	}
}
