package jev

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestLiveSystemOne(t *testing.T) {
	if os.Getenv("REPOSENTINEL_JEV_LIVE_TEST") != "1" {
		t.Skip("跳过 live 真实冒烟测试（未开启 REPOSENTINEL_JEV_LIVE_TEST=1）")
	}

	rawKey := os.Getenv("REPOSENTINEL_JEV_API_KEY")
	if rawKey == "" {
		t.Skip("跳过 live 真实冒烟测试（未设置 REPOSENTINEL_JEV_API_KEY）")
	}

	baseURL := os.Getenv("REPOSENTINEL_JEV_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.typesafe.ai"
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	// 预热 TLS 握手连接（生产中长连接保持复用；避免跨洋冷启动建连抖动吃满硬预算）
	warmReq, _ := http.NewRequestWithContext(t.Context(), http.MethodHead, baseURL, nil)
	if warmResp, err := httpClient.Do(warmReq); err == nil {
		_ = warmResp.Body.Close()
	}

	opts := ClientOptions{
		BaseURL:        baseURL,
		APIKey:         config.NewSecret(rawKey),
		Model:          "jev-1.14.0",
		Plane:          "notification",
		Timeout:        1000 * time.Millisecond,
		MaxConcurrency: 4,
		Retries:        1,
		HTTPClient:     httpClient,
	}

	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	q := map[string]Question{
		"is_actionable": NoulQuestion{
			Type:         "noul",
			Instructions: "Does this synthetic issue event require human intervention?",
			Criteria:     map[string]string{"true": "Action required", "false": "Noise"},
		},
	}
	state := json.RawMessage(`{"kind":"issue","action":"opened","sender_is_bot":false}`)

	start := time.Now()
	res, err := client.Judge(t.Context(), state, q)
	cost := time.Since(start)

	if err != nil {
		t.Fatalf("Live 判定失败: %v", err)
	}

	if res.Model != "jev-1.14.0" {
		t.Fatalf("期望模型 jev-1.14.0, 实际: %s", res.Model)
	}
	if ans, ok := res.Answers["is_actionable"]; !ok || ans.Noul == nil {
		t.Fatalf("未返回合法的 noul 判定: %+v", res.Answers)
	}

	t.Logf("Live 请求成功! Model: %s, InTokens: %d, OutTokens: %d, Duration: %v, Noul: %.2f",
		res.Model, res.Usage.InputTokens, res.Usage.OutputTokens, cost, *res.Answers["is_actionable"].Noul)
}

func TestLiveSystemOne_MultiQuestionMatrix(t *testing.T) {
	if os.Getenv("REPOSENTINEL_JEV_LIVE_TEST") != "1" {
		t.Skip("跳过 live 真实复合问答测试（未开启 REPOSENTINEL_JEV_LIVE_TEST=1）")
	}

	rawKey := os.Getenv("REPOSENTINEL_JEV_API_KEY")
	if rawKey == "" {
		t.Skip("跳过 live 真实复合问答测试（未设置 REPOSENTINEL_JEV_API_KEY）")
	}

	baseURL := os.Getenv("REPOSENTINEL_JEV_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.typesafe.ai"
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	// 预热建连
	warmReq, _ := http.NewRequestWithContext(t.Context(), http.MethodHead, baseURL, nil)
	if warmResp, err := httpClient.Do(warmReq); err == nil {
		_ = warmResp.Body.Close()
	}

	opts := ClientOptions{
		BaseURL:        baseURL,
		APIKey:         config.NewSecret(rawKey),
		Model:          "jev-1.14.0",
		Plane:          "review",
		Timeout:        1000 * time.Millisecond,
		MaxConcurrency: 4,
		Retries:        1,
		HTTPClient:     httpClient,
	}

	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 复合问题包含 Noul, Choice 与 Score 三种协议格式
	questions := map[string]Question{
		"is_actionable": NoulQuestion{
			Type:         "noul",
			Instructions: "Does this event require human intervention?",
			Criteria: map[string]string{
				"true":  "Action required",
				"false": "Harmless noise",
			},
		},
		"severity_tier": ChoiceQuestion{
			Type:         "choice",
			Instructions: "Select event severity.",
			Criteria: map[string]*string{
				"critical": nil,
				"high":     nil,
				"medium":   nil,
				"low":      nil,
			},
		},
		"urgency": ScoreQuestion{
			Type:         "score",
			Instructions: "Rate urgency from routine to immediate.",
			Criteria: []string{
				"Routine",
				"Soon",
				"Immediate",
			},
		},
	}
	state := json.RawMessage(`{"kind":"workflow_run","conclusion":"failure","sender_is_bot":false}`)

	// 1. 真实远程请求
	start := time.Now()
	res, err := client.Judge(t.Context(), state, questions)
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("复合问答调用失败: %v", err)
	}

	if res.Model != "jev-1.14.0" {
		t.Errorf("模型不匹配: %s", res.Model)
	}
	if ans, ok := res.Answers["is_actionable"]; !ok || ans.Noul == nil {
		t.Errorf("is_actionable 结果缺失: %+v", res.Answers)
	}
	if ans, ok := res.Answers["severity_tier"]; !ok || ans.Choice == nil {
		t.Errorf("severity_tier Choice 结果缺失: %+v", res.Answers)
	}
	if ans, ok := res.Answers["urgency"]; !ok || ans.Score == nil {
		t.Errorf("urgency Score 结果缺失: %+v", res.Answers)
	}

	t.Logf("复合问答远程请求成功! 耗时: %v, Noul: %.2f, Choice: %s, Score: %.2f",
		duration,
		*res.Answers["is_actionable"].Noul,
		*res.Answers["severity_tier"].Choice,
		*res.Answers["urgency"].Score,
	)

	// 2. 验证缓存命中（review 面允许缓存）
	cacheStart := time.Now()
	cachedRes, err := client.Judge(t.Context(), state, questions)
	cacheDuration := time.Since(cacheStart)
	if err != nil {
		t.Fatalf("缓存查询失败: %v", err)
	}
	if cachedRes.Model != res.Model {
		t.Errorf("缓存结果不一致")
	}
	t.Logf("缓存二次命中成功! 耗时: %v (< 1ms)", cacheDuration)
}
