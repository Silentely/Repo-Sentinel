package jev

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrMalformedDecision = errors.New("jev: malformed decision")
	ErrStateTooLarge     = errors.New("jev: state too large")
	ErrCircuitOpen       = errors.New("jev: circuit breaker open")
	ErrRateLimited       = errors.New("jev: rate limited")
	ErrTruncatedState    = errors.New("jev: truncated state must not be judged automatically")
)

// Question 表示向 Jev 提出的问题契约。
type Question interface {
	questionType() string
}

// NoulQuestion 专门用于 yes/no 门控裁决。
type NoulQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

func (q NoulQuestion) questionType() string { return "noul" }

// ChoiceQuestion 从候选集合中做互斥选择。
type ChoiceQuestion struct {
	Type         string             `json:"type"`
	Instructions string             `json:"instructions"`
	Criteria     map[string]*string `json:"criteria"`
}

func (q ChoiceQuestion) questionType() string { return "choice" }

// ScoreQuestion 按照有序等级进行评分。
type ScoreQuestion struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     []string `json:"criteria"`
}

func (q ScoreQuestion) questionType() string { return "score" }

// Request 表示向 Jev 发起的请求实体契约。
type Request struct {
	Model     string              `json:"model"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Answer 是 Jev 返回的单题回答联合类型。
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        *string            `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Usage 记录输入输出 Token 用量。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Result 是 Jev 响应的顶层实体。
type Result struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Client 定义 Jev 判别式决策客户端接口。
type Client interface {
	Judge(context.Context, json.RawMessage, map[string]Question) (Result, error)
}
