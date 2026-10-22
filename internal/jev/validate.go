package jev

import (
	"encoding/json"
	"math"
	"strconv"
)

// ValidateRequest 严格校验 SystemOne 请求实体是否合规。
func ValidateRequest(req Request) error {
	if req.Model == "" {
		return ErrMalformedDecision
	}
	if len(req.State) == 0 || string(req.State) == "null" || len(req.State) > MaxStateSize {
		return ErrMalformedDecision
	}
	// 校验 State 为合法 JSON
	var stateVal any
	if err := json.Unmarshal(req.State, &stateVal); err != nil {
		return ErrMalformedDecision
	}
	if len(req.Questions) == 0 {
		return ErrMalformedDecision
	}
	for _, q := range req.Questions {
		if q == nil {
			return ErrMalformedDecision
		}
		switch qTyped := q.(type) {
		case NoulQuestion:
			if qTyped.Type != "noul" || qTyped.Instructions == "" {
				return ErrMalformedDecision
			}
		case ChoiceQuestion:
			if qTyped.Type != "choice" || qTyped.Instructions == "" || len(qTyped.Criteria) < 2 {
				return ErrMalformedDecision
			}
		case ScoreQuestion:
			if qTyped.Type != "score" || qTyped.Instructions == "" || len(qTyped.Criteria) < 2 {
				return ErrMalformedDecision
			}
		default:
			return ErrMalformedDecision
		}
	}
	return nil
}

// DecodeRequest 从原始 JSON 解析并校验 SystemOne 请求（含 questions.<name>.type 校验）。
func DecodeRequest(raw []byte) (Request, error) {
	var top struct {
		Model     string                     `json:"model"`
		State     json.RawMessage            `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return Request{}, ErrMalformedDecision
	}
	if top.Model == "" || len(top.State) == 0 || string(top.State) == "null" || len(top.Questions) == 0 {
		return Request{}, ErrMalformedDecision
	}

	questions := make(map[string]Question, len(top.Questions))
	for k, qRaw := range top.Questions {
		var typeProbe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(qRaw, &typeProbe); err != nil || typeProbe.Type == "" {
			return Request{}, ErrMalformedDecision
		}
		switch typeProbe.Type {
		case "noul":
			var nq NoulQuestion
			if err := json.Unmarshal(qRaw, &nq); err != nil {
				return Request{}, ErrMalformedDecision
			}
			questions[k] = nq
		case "choice":
			var cq ChoiceQuestion
			if err := json.Unmarshal(qRaw, &cq); err != nil {
				return Request{}, ErrMalformedDecision
			}
			questions[k] = cq
		case "score":
			var sq ScoreQuestion
			if err := json.Unmarshal(qRaw, &sq); err != nil {
				return Request{}, ErrMalformedDecision
			}
			questions[k] = sq
		default:
			return Request{}, ErrMalformedDecision
		}
	}

	req := Request{
		Model:     top.Model,
		State:     top.State,
		Questions: questions,
	}
	if err := ValidateRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// ValidateResult 严格检验结果是否符合题目约束与数值合法性。
func ValidateResult(questions map[string]Question, result Result) error {
	if result.Model == "" {
		return ErrMalformedDecision
	}
	if result.Usage.InputTokens < 0 || result.Usage.OutputTokens < 0 {
		return ErrMalformedDecision
	}
	if result.Answers == nil {
		return ErrMalformedDecision
	}

	for qKey, question := range questions {
		ans, ok := result.Answers[qKey]
		if !ok {
			return ErrMalformedDecision
		}
		if ans.Type != question.questionType() {
			return ErrMalformedDecision
		}

		switch q := question.(type) {
		case NoulQuestion:
			if err := validateNoul(ans); err != nil {
				return err
			}
		case ChoiceQuestion:
			if err := validateChoice(q, ans); err != nil {
				return err
			}
		case ScoreQuestion:
			if err := validateScore(q, ans); err != nil {
				return err
			}
		default:
			return ErrMalformedDecision
		}
	}

	return nil
}

func validateNoul(ans Answer) error {
	if ans.Noul == nil {
		return ErrMalformedDecision
	}
	val := *ans.Noul
	if math.IsNaN(val) || math.IsInf(val, 0) || val < 0 || val > 1 {
		return ErrMalformedDecision
	}
	if ans.Confidence != nil {
		conf := *ans.Confidence
		if math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0 || conf > 1 {
			return ErrMalformedDecision
		}
	}
	return nil
}

func validateChoice(q ChoiceQuestion, ans Answer) error {
	if ans.Choice == nil || ans.Confidence == nil || ans.Probabilities == nil {
		return ErrMalformedDecision
	}
	choice := *ans.Choice
	if _, ok := q.Criteria[choice]; !ok {
		return ErrMalformedDecision
	}

	conf := *ans.Confidence
	if math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0 || conf > 1 {
		return ErrMalformedDecision
	}

	if len(ans.Probabilities) != len(q.Criteria) {
		return ErrMalformedDecision
	}

	sum := 0.0
	maxProb := -1.0
	for opt := range q.Criteria {
		prob, ok := ans.Probabilities[opt]
		if !ok || math.IsNaN(prob) || math.IsInf(prob, 0) || prob < 0 || prob > 1 {
			return ErrMalformedDecision
		}
		sum += prob
		if prob > maxProb {
			maxProb = prob
		}
	}

	if math.Abs(sum-1.0) > 1e-6 {
		return ErrMalformedDecision
	}

	choiceProb, ok := ans.Probabilities[choice]
	if !ok || choiceProb < maxProb-1e-6 {
		return ErrMalformedDecision
	}

	return nil
}

func validateScore(q ScoreQuestion, ans Answer) error {
	if ans.Score == nil || ans.Confidence == nil || ans.Legend == nil || ans.Probabilities == nil {
		return ErrMalformedDecision
	}
	n := len(q.Criteria)
	if n < 2 {
		return ErrMalformedDecision
	}

	score := *ans.Score
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > float64(n-1) {
		return ErrMalformedDecision
	}

	conf := *ans.Confidence
	if math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0 || conf > 1 {
		return ErrMalformedDecision
	}

	if len(ans.Legend) != n || len(ans.Probabilities) != n {
		return ErrMalformedDecision
	}

	weighted := 0.0
	probSum := 0.0
	for i, expectedDesc := range q.Criteria {
		key := strconv.Itoa(i)
		desc, ok := ans.Legend[key]
		if !ok || desc != expectedDesc {
			return ErrMalformedDecision
		}
		prob, ok := ans.Probabilities[key]
		if !ok || math.IsNaN(prob) || math.IsInf(prob, 0) || prob < 0 || prob > 1 {
			return ErrMalformedDecision
		}
		probSum += prob
		weighted += float64(i) * prob
	}

	if math.Abs(probSum-1.0) > 1e-6 {
		return ErrMalformedDecision
	}

	if math.Abs(score-weighted) > 1e-6 {
		return ErrMalformedDecision
	}

	return nil
}

// DecodeResult 从原始 JSON 中解码并进行全量结构与业务严格校验。
// 任何缺字段、类型不符、非法数值均返回 ErrMalformedDecision，绝不默认放行。
func DecodeResult(raw []byte, questions map[string]Question, model string) (Result, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Result{}, ErrMalformedDecision
	}

	for _, reqKey := range []string{"model", "answers", "usage"} {
		msg, ok := top[reqKey]
		if !ok || len(msg) == 0 || string(msg) == "null" {
			return Result{}, ErrMalformedDecision
		}
	}

	// 严格校验 usage 字段结构（input_tokens 与 output_tokens 均非空且为非负整数）
	var usageCheck struct {
		InputTokens  *int `json:"input_tokens"`
		OutputTokens *int `json:"output_tokens"`
	}
	if err := json.Unmarshal(top["usage"], &usageCheck); err != nil || usageCheck.InputTokens == nil || usageCheck.OutputTokens == nil {
		return Result{}, ErrMalformedDecision
	}
	if *usageCheck.InputTokens < 0 || *usageCheck.OutputTokens < 0 {
		return Result{}, ErrMalformedDecision
	}

	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, ErrMalformedDecision
	}

	if model != "" && res.Model != model {
		return Result{}, ErrMalformedDecision
	}

	if err := ValidateResult(questions, res); err != nil {
		return Result{}, err
	}

	return res, nil
}
