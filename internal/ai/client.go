package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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

	// sem 为调用并发预算（#8）：digest 多周期与分诊可能并发打 AI，
	// 超出上限时排队（受 ctx 预算约束）。非配置字段，经 Replace 热更新不受影响；
	// 懒初始化（semChan），避免构造函数侵入。Channel 为引用类型，Snapshot 复制后共享同一信号量。
	sem chan struct{}
}

// aiMaxConcurrency AI 调用并发上限：摘要/分诊/连通性共用一个客户端实例时
// 限制同时在途的 LLM 请求，避免突发调用推高网关延迟与费用。
// 设为 var 便于测试调整；后续可配置化。
var aiMaxConcurrency = 2

// semChan 返回并发预算信号量，首次调用时懒初始化（受 c.mu 保护）。
func (c *Client) semChan() chan struct{} {
	c.mu.Lock()
	if c.sem == nil {
		c.sem = make(chan struct{}, aiMaxConcurrency)
	}
	sem := c.sem
	c.mu.Unlock()
	return sem
}

// acquireSlot 获取一个 AI 调用并发槽位；无空位时阻塞等待，直到有槽或 ctx 结束。
// 等待计入调用总时长，由调用方预算（digest 无硬限、分诊 15s）兜底。
func (c *Client) acquireSlot(ctx context.Context) (release func(), err error) {
	sem := c.semChan()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, &callError{code: "concurrency_limit", err: fmt.Errorf("ai: wait for concurrency slot: %w", ctx.Err())}
	}
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
	// Usage 为 OpenAI 兼容响应中的 token 用量（部分网关不返回，缺省为 0）。
	Usage chatUsage `json:"usage"`
}

// chatUsage 单次调用的 token 用量，供成本记账与日志留痕。
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// reqIDCtxKey 是请求关联 ID 的 context 键。
type reqIDCtxKey struct{}

// NewRequestID 生成 16 位十六进制请求关联 ID；随机源不可用时回退时间戳，保证非空。
func NewRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// WithRequestID 返回携带请求关联 ID 的 context，供上层在参与度日志与调用日志间串联。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDCtxKey{}, id)
}

// RequestIDFromContext 读取请求关联 ID；未注入时返回空串。
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(reqIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// EnsureRequestID 保证 ctx 携带请求 ID：已有则沿用，否则生成新 ID 注入。
// 返回的 ID 供上层参与度日志使用，与 ai 层调用日志的 req_id 保持一致，
// 使「参与度 → 调用 → 结果」可按单次决策端到端串联。
func EnsureRequestID(ctx context.Context) (context.Context, string) {
	if id := RequestIDFromContext(ctx); id != "" {
		return ctx, id
	}
	id := NewRequestID()
	return WithRequestID(ctx, id), id
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
// 请求成败统一留痕（Logger 注入时）：DEBUG 记录发起，INFO 记录成功与输出长度
// 及 token 用量，WARN 记录失败并附 error_code 分类（timeout/network/upstream_<status>
// 等），便于区分网络问题与上游服务问题；所有日志携带 req_id（context 未注入时自动生成），
// 与上层参与度日志串联。未配置时不发起请求也不留痕（上层按降级处理）。
// 指标与日志同源：成功/失败/耗时/token 在出口统一累计（见 metrics.go）。
func (c *Client) Complete(ctx context.Context, system, user string) (content string, err error) {
	s := c.Snapshot()
	if !s.Enabled || s.APIKey == "" {
		return "", ErrNotConfigured
	}
	logger := s.Logger
	model := s.model()
	reqID := RequestIDFromContext(ctx)
	if reqID == "" {
		reqID = NewRequestID()
	}
	start := time.Now()
	var promptTok, completionTok int
	// 成功与失败统一在出口留痕与计数，避免各分支重复记录。
	defer func() {
		if err != nil {
			code, detail := classifyCallError(err)
			metricsIncResult(true, code, time.Since(start), 0, 0)
			if logger != nil {
				logger.Warn("ai request failed",
					"req_id", reqID, "model", model, "duration_ms", time.Since(start).Milliseconds(),
					"error_code", code, "error", detail)
			}
			return
		}
		metricsIncResult(false, "", time.Since(start), promptTok, completionTok)
		if logger != nil {
			logger.Info("ai request ok",
				"req_id", reqID, "model", model, "duration_ms", time.Since(start).Milliseconds(),
				"output_chars", len(content),
				"prompt_tokens", promptTok, "completion_tokens", completionTok)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	// 并发预算：超出上限时排队，等待计入总时长（受 ctx 预算约束）。
	release, err := c.acquireSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()

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
			"req_id", reqID,
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
	promptTok, completionTok = out.Usage.PromptTokens, out.Usage.CompletionTokens
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
