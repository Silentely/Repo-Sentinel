package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// 健康检查请求超时；须小于 Dockerfile HEALTHCHECK 的 --timeout，避免探针未及响应即被判定失败。
	healthcheckTimeout = 3 * time.Second
	// REPOSENTINEL_HTTP_ADDR 未设置或无法解析时的回退端口，与镜像 EXPOSE 端口一致。
	defaultHealthcheckPort = "8080"
)

// runHealthcheck 探测本机 /health/ready：返回 200 视为就绪退出码 0，其余一律退出码 1。
// 供 distroless 镜像（无 shell、无 curl）的 HEALTHCHECK 直接执行，仅依赖静态二进制自身。
func (r Runner) runHealthcheck(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return newCLIError("healthcheck 不接受额外参数。")
	}
	url := "http://127.0.0.1:" + healthcheckPort(os.Getenv("REPOSENTINEL_HTTP_ADDR")) + "/health/ready"
	client := &http.Client{Timeout: healthcheckTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cliError{code: "healthcheck_failed", message: "健康检查请求构造失败。"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return cliError{code: "healthcheck_failed", message: "就绪探针不可达。"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cliError{code: "healthcheck_failed", message: fmt.Sprintf("就绪探针状态码=%d。", resp.StatusCode)}
	}
	fmt.Fprintf(r.stdout, "ready=ok url=%s\n", url)
	return nil
}

// healthcheckPort 从 host:port 或 :port 形式的监听地址解析端口；解析失败回退默认端口。
func healthcheckPort(addr string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return defaultHealthcheckPort
	}
	return port
}
