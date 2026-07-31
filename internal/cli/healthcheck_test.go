package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// useHealthcheckServer 以 httptest.Server 模拟本机就绪探针，并把服务端口注入 REPOSENTINEL_HTTP_ADDR。
// 返回探针实际收到的请求路径，用于断言子命令访问的是 /health/ready。
func useHealthcheckServer(t *testing.T, status int) *string {
	t.Helper()
	requested := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requested = r.URL.Path
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	t.Setenv("REPOSENTINEL_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
	return requested
}

func TestRunner健康检查探针就绪时成功(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	requested := useHealthcheckServer(t, http.StatusOK)
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{})
	if err := runner.Run(t.Context(), []string{"healthcheck"}); err != nil {
		t.Fatalf("healthcheck 失败: %v", err)
	}
	if *requested != "/health/ready" {
		t.Fatalf("探针路径=%q，期望 /health/ready", *requested)
	}
	if !strings.Contains(stdout.String(), "ready=ok") {
		t.Fatalf("成功输出缺少 ready=ok: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("healthcheck stderr=%q，期望为空", stderr.String())
	}
}

func TestRunner健康检查探针非200时失败(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	useHealthcheckServer(t, http.StatusInternalServerError)
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{})
	if err := runner.Run(t.Context(), []string{"healthcheck"}); err == nil {
		t.Fatal("探针返回 500 时 healthcheck 应失败")
	}
	if !strings.Contains(stderr.String(), "error_code=healthcheck_failed") {
		t.Fatalf("stderr 缺少 healthcheck_failed: %s", stderr.String())
	}
}

func TestRunner健康检查服务不可达时失败(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	// 借用一个随即关闭的端口，保证目标地址连接被拒。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("分配空闲端口失败: %v", err)
	}
	t.Setenv("REPOSENTINEL_HTTP_ADDR", listener.Addr().String())
	if err := listener.Close(); err != nil {
		t.Fatalf("释放端口失败: %v", err)
	}
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{})
	if err := runner.Run(t.Context(), []string{"healthcheck"}); err == nil {
		t.Fatal("服务不可达时 healthcheck 应失败")
	}
	if !strings.Contains(stderr.String(), "error_code=healthcheck_failed") {
		t.Fatalf("stderr 缺少 healthcheck_failed: %s", stderr.String())
	}
}

func TestRunner健康检查拒绝额外参数(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(strings.NewReader(""), &stdout, &stderr, Dependencies{})
	if err := runner.Run(t.Context(), []string{"healthcheck", "--verbose"}); err == nil {
		t.Fatal("额外参数应被拒绝")
	}
}

func TestHealthcheckPort解析(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:9090": "9090",
		"0.0.0.0:8080":   "8080",
		":7070":          "7070",
		"":               "8080",
		"garbage":        "8080",
		"127.0.0.1":      "8080",
	}
	for addr, expected := range cases {
		if got := healthcheckPort(addr); got != expected {
			t.Errorf("healthcheckPort(%q)=%q，期望 %q", addr, got, expected)
		}
	}
}
