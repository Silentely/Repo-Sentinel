package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"golang.org/x/sync/singleflight"
)

// ClientOptions 注入客户端运行时参数与依赖。
type ClientOptions struct {
	BaseURL              string
	APIKey               config.Secret
	Model                string
	Plane                string
	PolicyVersion        string
	CredentialGeneration uint64
	RuntimeGeneration    uint64
	Timeout              time.Duration
	MaxConcurrency       int
	Retries              int
	HTTPClient           *http.Client
	Now                  func() time.Time
	Jitter               func() float64
	Breaker              BreakerConfig
}

type clientImpl struct {
	opts          ClientOptions
	httpClient    *http.Client
	breaker       *circuitBreaker
	cache         *decisionCache
	flight        *singleflight.Group
	semaphore     chan struct{}
	now           func() time.Time
	jitter        func() float64
	mu            sync.Mutex
	nextAllowedAt time.Time
}

// NormalizeEndpoint 对输入的 Base URL 智能归一化，支持传入根路径、带 /v1 或完整 /v1/systemone 端点。
func NormalizeEndpoint(baseURL string) string {
	clean := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(clean, "/v1/systemone") {
		return clean
	}
	if strings.HasSuffix(clean, "/v1") {
		return clean + "/systemone"
	}
	return clean + "/v1/systemone"
}

// NewClient 构造带故障隔离、硬预算与连接超时的 Jev 客户端实例。
func NewClient(options ClientOptions) (Client, error) {
	if options.BaseURL == "" {
		return nil, errors.New("jev: base_url is required")
	}
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || (parsed.Scheme != "https" && options.HTTPClient == nil) {
		return nil, errors.New("jev: base_url must be https")
	}
	if options.Timeout <= 0 || options.Timeout > 60*time.Second {
		options.Timeout = 30 * time.Second
	}
	if options.MaxConcurrency <= 0 || options.MaxConcurrency > 32 {
		options.MaxConcurrency = 8
	}
	if options.Retries < 0 || options.Retries > 1 {
		options.Retries = 1
	}
	if options.PolicyVersion == "" {
		options.PolicyVersion = "jev-policy-v1"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	jitter := options.Jitter
	if jitter == nil {
		jitter = func() float64 {
			return rand.Float64()*2 - 1
		}
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		connectTimeout := options.Timeout / 2
		if connectTimeout <= 0 || connectTimeout > 5*time.Second {
			connectTimeout = 5 * time.Second
		}
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: options.Timeout,
			MaxConnsPerHost:       options.MaxConcurrency,
			MaxIdleConnsPerHost:   options.MaxConcurrency,
			IdleConnTimeout:       90 * time.Second,
		}
		httpClient = &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	return &clientImpl{
		opts:       options,
		httpClient: httpClient,
		breaker:    newBreaker(now, options.Breaker),
		cache: newCacheWithOnEvict(5000, 10*time.Minute, now, jitter, func() {
			GlobalMetrics().RecordCacheEviction(options.Plane)
		}),
		flight:    new(singleflight.Group),
		semaphore: make(chan struct{}, options.MaxConcurrency),
		now:       now,
		jitter:    jitter,
	}, nil
}

func (c *clientImpl) BreakerStats() BreakerStats {
	if c.breaker == nil {
		return BreakerStats{State: "closed"}
	}
	return c.breaker.getStats()
}

func (c *clientImpl) CacheStats() CacheStats {
	if c.cache == nil {
		return CacheStats{}
	}
	return c.cache.stats()
}

func (c *clientImpl) Judge(ctx context.Context, state json.RawMessage, questions map[string]Question) (Result, error) {
	GlobalMetrics().RecordCall(c.opts.Plane)
	start := c.now()
	defer func() {
		costMs := float64(c.now().Sub(start).Microseconds()) / 1000.0
		GlobalMetrics().RecordLatency(c.opts.Plane, costMs)
	}()

	// 状态大小硬限制 8KiB
	if len(state) > MaxStateSize {
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "decode")
		return Result{}, ErrStateTooLarge
	}

	// 规则 6：截断必须保守终局，截断状态严禁放行，必须判定为失败并升级人工
	if IsTruncatedState(state) {
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "truncated")
		return Result{}, ErrTruncatedState
	}

	// 预估问题大小限制 8KiB
	rawQ, err := json.Marshal(questions)
	if err != nil || len(rawQ) > MaxStateSize {
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "decode")
		return Result{}, ErrStateTooLarge
	}

	// 预算硬上限 min(父 ctx deadline, 面配置预算, 允许上限 60s)
	maxBudget := 60 * time.Second
	budget := c.opts.Timeout
	if budget <= 0 || budget > maxBudget {
		budget = maxBudget
	}
	if parentDeadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(parentDeadline)
		if remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "timeout")
		return Result{}, context.DeadlineExceeded
	}

	waitCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	isChatOps := c.opts.Plane == "chatops"
	cacheable := !isChatOps && IsCacheable(state)
	var cacheKey string

	if cacheable {
		var keyErr error
		cacheKey, keyErr = CanonicalKey(c.opts, state, questions)
		if keyErr != nil {
			GlobalMetrics().RecordFailure(c.opts.Plane)
			GlobalMetrics().RecordFailureClass(c.opts.Plane, "decode")
			return Result{}, keyErr
		}
		if cached, hit := c.cache.get(cacheKey); hit {
			GlobalMetrics().RecordCacheHit(c.opts.Plane)
			GlobalMetrics().RecordSuccess(c.opts.Plane)
			return cached, nil
		}
		GlobalMetrics().RecordCacheMiss(c.opts.Plane)
	}

	// 限流与熔断快速短路
	c.mu.Lock()
	if c.now().Before(c.nextAllowedAt) {
		c.mu.Unlock()
		GlobalMetrics().RecordFallback(c.opts.Plane)
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "rate_limited")
		return Result{}, ErrRateLimited
	}
	c.mu.Unlock()

	// 工作任务执行闭包：在闭包内部原子获取 permit，确保仅真正执行网络请求的 runner 占用半开名额
	executeWork := func(workCtx context.Context) (Result, error) {
		permit := c.breaker.AcquirePermit()
		if !permit.Allowed {
			return Result{}, ErrCircuitOpen
		}
		permitHandled := false
		defer func() {
			if !permitHandled {
				c.breaker.ReleasePermit(permit)
			}
		}()

		// 获取并发槽位（等待槽位超时不记断路器错误，退出时释放名额）
		select {
		case c.semaphore <- struct{}{}:
			defer func() { <-c.semaphore }()
		case <-workCtx.Done():
			return Result{}, workCtx.Err()
		}

		res, remoteErr := c.doRequestWithRetry(workCtx, state, questions)
		if remoteErr != nil {
			permitHandled = true
			// 仅上游真正的失败（5xx、网络中断、协议畸形）计入断路器失败
			if errors.Is(remoteErr, ErrMalformedDecision) || isUpstreamFailure(remoteErr) {
				c.breaker.RecordPermitResult(permit, false)
			} else {
				c.breaker.ReleasePermit(permit)
			}
			return Result{}, remoteErr
		}

		permitHandled = true
		c.breaker.RecordPermitResult(permit, true)
		return res, nil
	}

	// ChatOps 或非可缓存（敏感/超长截断）请求：严禁并发单飞，直通执行
	if isChatOps || !cacheable {
		res, err := executeWork(waitCtx)
		if err != nil {
			if errors.Is(err, ErrCircuitOpen) {
				GlobalMetrics().RecordFallback(c.opts.Plane)
				GlobalMetrics().RecordFailure(c.opts.Plane)
				GlobalMetrics().RecordFailureClass(c.opts.Plane, "breaker_open")
				return Result{}, err
			}
			GlobalMetrics().RecordFailure(c.opts.Plane)
			GlobalMetrics().RecordFailureClass(c.opts.Plane, ClassifyError(err))
			return Result{}, err
		}
		GlobalMetrics().RecordSuccess(c.opts.Plane)
		return res, nil
	}

	// 非 ChatOps 可缓存请求：通过 singleflight.DoChan 抑制并发重复查询
	// 阻断项 1 & 2：共享任务运行在独立 context（不受首调用者提前取消牵连），但遵守配置的最大预算
	sharedBudget := c.opts.Timeout
	if sharedBudget <= 0 || sharedBudget > maxBudget {
		sharedBudget = maxBudget
	}
	ch := c.flight.DoChan(cacheKey, func() (interface{}, error) {
		sharedCtx, sharedCancel := context.WithTimeout(context.Background(), sharedBudget)
		defer sharedCancel()
		return executeWork(sharedCtx)
	})

	select {
	case <-waitCtx.Done():
		GlobalMetrics().RecordFailure(c.opts.Plane)
		GlobalMetrics().RecordFailureClass(c.opts.Plane, "timeout")
		return Result{}, waitCtx.Err()
	case res := <-ch:
		if res.Err != nil {
			if errors.Is(res.Err, ErrCircuitOpen) {
				GlobalMetrics().RecordFallback(c.opts.Plane)
				GlobalMetrics().RecordFailure(c.opts.Plane)
				GlobalMetrics().RecordFailureClass(c.opts.Plane, "breaker_open")
				return Result{}, res.Err
			}
			GlobalMetrics().RecordFailure(c.opts.Plane)
			GlobalMetrics().RecordFailureClass(c.opts.Plane, ClassifyError(res.Err))
			return Result{}, res.Err
		}
		result := res.Val.(Result)
		// 成功且非空写入缓存
		c.cache.put(cacheKey, result)
		GlobalMetrics().RecordSuccess(c.opts.Plane)
		return result, nil
	}
}

func (c *clientImpl) doRequestWithRetry(ctx context.Context, state json.RawMessage, questions map[string]Question) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}

	parsedQuestions := make(map[string]interface{})
	for k, q := range questions {
		parsedQuestions[k] = q
	}

	bodyData := map[string]interface{}{
		"model":     c.opts.Model,
		"state":     state,
		"questions": parsedQuestions,
	}

	bodyBytes, err := json.Marshal(bodyData)
	if err != nil {
		return Result{}, err
	}

	maxAttempts := 1 + c.opts.Retries
	endpoint := NormalizeEndpoint(c.opts.BaseURL)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return Result{}, err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", buildinfo.UserAgent("JevClient"))
		if apiKey := c.opts.APIKey.Reveal(); apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// context 超时/取消不重试
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return Result{}, ctx.Err()
				}
				return Result{}, err
			}
			if attempt+1 < maxAttempts {
				continue
			}
			return Result{}, err
		}

		// 处理 429 与 529
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			retryAfterHeader := resp.Header.Get("Retry-After")
			_ = resp.Body.Close()

			retryDuration := parseRetryAfter(retryAfterHeader, c.now())
			if retryDuration <= 0 {
				retryDuration = 100 * time.Millisecond
			}

			c.mu.Lock()
			candidate := c.now().Add(retryDuration)
			if candidate.After(c.nextAllowedAt) {
				c.nextAllowedAt = candidate
			}
			c.mu.Unlock()

			// 规则 10：Retry-After 很长（>= 15s）时触发熔断保护，避免大量请求空转
			if retryDuration >= 15*time.Second {
				c.breaker.TripOpen(retryDuration)
			}

			if deadline, ok := ctx.Deadline(); ok {
				if time.Until(deadline) < retryDuration+200*time.Millisecond {
					return Result{}, ErrRateLimited
				}
			}
			if attempt+1 < maxAttempts {
				select {
				case <-time.After(retryDuration):
					continue
				case <-ctx.Done():
					return Result{}, ctx.Err()
				}
			}
			return Result{}, ErrRateLimited
		}

		// 5xx (500, 502, 503, 504 等) 可重试
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			if attempt+1 < maxAttempts {
				continue
			}
			return Result{}, fmt.Errorf("jev: server returned %d", resp.StatusCode)
		}

		// 其它 4xx (401, 403, 422 等) 不重试
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			return Result{}, fmt.Errorf("jev: client error %d", resp.StatusCode)
		}

		// 响应体上限 64KiB
		limitedReader := io.LimitReader(resp.Body, 64*1024+1)
		respBytes, err := io.ReadAll(limitedReader)
		_ = resp.Body.Close()
		if err != nil {
			return Result{}, err
		}
		if len(respBytes) > 64*1024 {
			return Result{}, ErrMalformedDecision
		}

		res, err := DecodeResult(respBytes, questions, c.opts.Model)
		if err != nil {
			return Result{}, err
		}
		return res, nil
	}

	return Result{}, errors.New("jev: request failed after retries")
}

func isUpstreamFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "server returned") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "EOF")
}
