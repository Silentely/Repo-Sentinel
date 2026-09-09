package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIGzipCompression 验证 API 端点与元数据端点在客户端携带 Accept-Encoding: gzip 时正确返回 gzip 压缩内容。
func TestAPIGzipCompression(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	server := httptest.NewServer(fixture.handler)
	defer server.Close()

	// 1. 测试 /openapi.json 端点 gzip 压缩
	req, err := http.NewRequest(http.MethodGet, server.URL+"/openapi.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("请求 /openapi.json 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码期望 200，实际: %d", resp.StatusCode)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("/openapi.json Content-Encoding 期望 gzip，实际: %q", enc)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("无法创建 gzip.Reader: %v", err)
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("解压响应体失败: %v", err)
	}
	var openapiSpec map[string]any
	if err := json.Unmarshal(decompressed, &openapiSpec); err != nil {
		t.Fatalf("解压后 JSON 解析失败: %v", err)
	}
	if _, ok := openapiSpec["openapi"]; !ok {
		t.Fatalf("解压后规范缺少 openapi 字段")
	}

	// 2. 测试 /api/v1/system/build-info 端点 gzip 压缩
	reqAPI, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/system/build-info", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqAPI.Header.Set("Accept-Encoding", "gzip")

	respAPI, err := server.Client().Do(reqAPI)
	if err != nil {
		t.Fatalf("请求 /api/v1/system/build-info 失败: %v", err)
	}
	defer respAPI.Body.Close()

	if respAPI.StatusCode != http.StatusOK {
		t.Fatalf("状态码期望 200，实际: %d", respAPI.StatusCode)
	}
	if enc := respAPI.Header.Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("/api/v1/system/build-info Content-Encoding 期望 gzip，实际: %q", enc)
	}

	gzReaderAPI, err := gzip.NewReader(respAPI.Body)
	if err != nil {
		t.Fatalf("无法创建 gzip.Reader: %v", err)
	}
	defer gzReaderAPI.Close()

	decompressedAPI, err := io.ReadAll(gzReaderAPI)
	if err != nil {
		t.Fatalf("解压 API 响应体失败: %v", err)
	}
	var buildInfo map[string]any
	if err := json.Unmarshal(decompressedAPI, &buildInfo); err != nil {
		t.Fatalf("解压后 JSON 解析失败: %v", err)
	}
	if _, ok := buildInfo["version"]; !ok {
		t.Fatalf("解压后缺少 version 字段: %s", string(decompressedAPI))
	}
}
