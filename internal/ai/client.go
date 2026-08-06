package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured 表示未配置 API Key；调用方应视为功能不可用并降级，不视为故障。
var ErrNotConfigured = errors.New("ai: not configured")

// 未显式配置时的使用点回退默认值：管理台/环境变量均未设置时生效，
// 供客户端方法与连通性测试（httpapi）共用，保持单一来源。
const (
	DefaultBaseURL   = "https://api.openai.com/v1"
	DefaultModel     = "gpt-4o-mini"
	DefaultTimeout   = 30 * time.Second
	DefaultMaxTokens = 800
)

// Client 是 OpenAI 兼容 Chat Completions 客户端（BYOK）。
// 通过可配置的 BaseURL 可接入任意 OpenAI 兼容网关（含 Ollama / vLLM 等本地模型）。
// 字段经 mu 保护：构造后通过 Replace 热更新，读侧一律走 Snapshot。
type Client struct {
	mu sync.RWMutex

	// Enabled 配置总开关；false 时不发起任何请求（即使已配置 API Key）。
	Enabled bool
	// BaseURL 形如 https://api.openai.com/v1，末尾斜杠会被裁剪。
	BaseURL string
	// APIKey 为空时功能不可用。
	APIKey string
	// Model 模型名；为空时使用缺省 gpt-4o-mini。
	Model string
	// Timeout 单次请求超时上限；为空时使用 30s。
	Timeout time.Duration
	// MaxTokens 输出 token 上限；为空时使用 800。
	MaxTokens int
	// DigestEnabled / TriageEnabled 分别控制摘要与分诊功能开关。
	DigestEnabled bool
	TriageEnabled bool

	// HTTP 可注入自定义客户端（测试/代理场景）。
	HTTP *http.Client
	// Logger 可选；AI 降级时记录原因，不影响主链路。
	Logger *slog.Logger
}

// Snapshot 返回当前只读副本，供方法与外部并发读取使用。
func (c *Client) Snapshot() Client {
	if c == nil {
		return Client{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Client{
		Enabled:       c.Enabled,
		BaseURL:       c.BaseURL,
		APIKey:        c.APIKey,
		Model:         c.Model,
		Timeout:       c.Timeout,
		MaxTokens:     c.MaxTokens,
		DigestEnabled: c.DigestEnabled,
		TriageEnabled: c.TriageEnabled,
		HTTP:          c.HTTP,
		Logger:        c.Logger,
	}
}

// Replace 用完整快照替换可变字段（保留未注入的 HTTP/Logger 现状）。
// 供管理台保存 AI 配置后热更新，避免重启进程。
func (c *Client) Replace(next *Client) {
	if c == nil || next == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = next.Enabled
	c.BaseURL = next.BaseURL
	c.APIKey = next.APIKey
	c.Model = next.Model
	c.Timeout = next.Timeout
	c.MaxTokens = next.MaxTokens
	c.DigestEnabled = next.DigestEnabled
	c.TriageEnabled = next.TriageEnabled
	if next.HTTP != nil {
		c.HTTP = next.HTTP
	}
	if next.Logger != nil {
		c.Logger = next.Logger
	}
}

// IsEnabled 判定 AI 总开关是否打开且已配置 API Key。
func (c *Client) IsEnabled() bool {
	s := c.Snapshot()
	return s.Enabled && s.APIKey != ""
}

// IsDigestEnabled 判定 AI 摘要（日/周/月报告）是否可用。
func (c *Client) IsDigestEnabled() bool {
	s := c.Snapshot()
	return s.Enabled && s.APIKey != "" && s.DigestEnabled
}

// IsTriageEnabled 判定实时安全告警分诊是否可用。
func (c *Client) IsTriageEnabled() bool {
	s := c.Snapshot()
	return s.Enabled && s.APIKey != "" && s.TriageEnabled
}

// defaultHTTPClient 包级共享默认客户端：复用连接池，避免每次请求新建。
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTPClient
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) model() string {
	if c.Model == "" {
		return DefaultModel
	}
	return c.Model
}

func (c *Client) maxTokens() int {
	if c.MaxTokens <= 0 {
		return DefaultMaxTokens
	}
	return c.MaxTokens
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// callError 携带调用失败分类与可选 HTTP 状态码，供日志输出稳定的 error_code。
// 调用方无需感知：错误文本仍保持 ai: 前缀，errors.Is/As 语义不变。
type callError struct {
	code   string
	status int // 上游 HTTP 状态码；非上游错误时为 0
	err    error
}

func (e *callError) Error() string { return e.err.Error() }
func (e *callError) Unwrap() error { return e.err }

// classifyCallError 提取失败分类与可读详情，供日志留痕。
// 分类回答「是网络问题还是上游问题」：timeout / network / upstream_<status> /
// bad_response / empty_response / internal；无法归类时返回 unknown。
func classifyCallError(err error) (code, detail string) {
	var ce *callError
	if errors.As(err, &ce) {
		if ce.status > 0 {
			return fmt.Sprintf("upstream_%d", ce.status), err.Error()
		}
		return ce.code, err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", err.Error()
	}
	return "unknown", err.Error()
}

// Complete 执行单轮对话并返回助手文本。
// 未配置、网络失败、HTTP 非 2xx、响应缺内容均返回错误，调用方负责降级。
// 请求成败统一留痕（Logger 注入时）：DEBUG 记录发起，INFO 记录成功与输出长度，
// WARN 记录失败并附 error_code 分类（timeout/network/upstream_<status> 等），
// 便于区分网络问题与上游服务问题。未配置时不发起请求也不留痕（上层按降级处理）。
func (c *Client) Complete(ctx context.Context, system, user string) (content string, err error) {
	s := c.Snapshot()
	if !s.Enabled || s.APIKey == "" {
		return "", ErrNotConfigured
	}
	logger := s.Logger
	model := s.model()
	start := time.Now()
	// 成功与失败统一在出口留痕，避免各分支重复记录。
	defer func() {
		if logger == nil {
			return
		}
		attrs := []any{"model", model, "duration_ms", time.Since(start).Milliseconds()}
		if err != nil {
			code, detail := classifyCallError(err)
			logger.Warn("ai request failed", append(attrs, "error_code", code, "error", detail)...)
			return
		}
		logger.Info("ai request ok", append(attrs, "output_chars", len(content))...)
	}()

	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	payload, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens:   s.maxTokens(),
		Temperature: 0.3,
	})
	if err != nil {
		return "", &callError{code: "internal", err: fmt.Errorf("ai: encode request: %w", err)}
	}

	endpoint := s.baseURL() + "/chat/completions"
	if logger != nil {
		logger.Debug("ai request start",
			"model", model,
			"endpoint", endpoint,
			"max_tokens", s.maxTokens(),
			"timeout_ms", s.timeout().Milliseconds(),
			"input_bytes", len(system)+len(user),
		)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", &callError{code: "internal", err: fmt.Errorf("ai: build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.APIKey)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", &callError{code: "timeout", err: fmt.Errorf("ai: request timeout after %s: %w", s.timeout(), err)}
		}
		return "", &callError{code: "network", err: fmt.Errorf("ai: request failed: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &callError{code: "upstream", status: resp.StatusCode, err: fmt.Errorf("ai: http %d: %s", resp.StatusCode, errorDetail(resp))}
	}

	var out chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return "", &callError{code: "bad_response", err: fmt.Errorf("ai: decode response: %w", err)}
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", &callError{code: "empty_response", err: errors.New("ai: empty response")}
	}
	return truncateOutput(strings.TrimSpace(out.Choices[0].Message.Content)), nil
}

// maxResponseBytes AI 响应体大小上限：防御异常大响应消耗内存。
const maxResponseBytes = 1 << 20 // 1 MiB

// errorDetail 提取非 2xx 响应体前 200 字符作为错误明细（如网关 502/503 附带的说明），
// 帮助连通性测试与降级日志定位真实原因；正文缺失或不可读时返回稳定占位。
func errorDetail(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "unreadable response"
	}
	detail := strings.TrimSpace(string(raw))
	if detail == "" {
		return "empty response"
	}
	const maxDetailRunes = 200
	if r := []rune(detail); len(r) > maxDetailRunes {
		detail = string(r[:maxDetailRunes]) + "…"
	}
	return detail
}

// maxOutputRunes AI 输出截断上限：Telegram 单条消息上限 4096 字符，
// 预留模板头部与 HTML 转义余量，防止超长输出导致投递失败。
const maxOutputRunes = 3500

// truncateOutput 按 rune 截断 AI 输出到安全长度（正常输出远低于上限，触顶时保前缀）。
func truncateOutput(content string) string {
	if r := []rune(content); len(r) > maxOutputRunes {
		return string(r[:maxOutputRunes])
	}
	return content
}

// Ping 发送一次最小对话验证连通性，返回端到端耗时。
// 未配置（无 API Key）返回 ErrNotConfigured；网络失败、HTTP 非 2xx、空响应返回错误。
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	s := c.Snapshot()
	if !s.Enabled || s.APIKey == "" {
		return 0, ErrNotConfigured
	}
	start := time.Now()
	_, err := c.Complete(ctx, "你是连通性测试助手，请只回复 OK。", "连通性测试：请回复 OK。")
	if err != nil {
		return time.Since(start), err
	}
	return time.Since(start), nil
}
