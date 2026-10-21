package jev

import (
	"encoding/json"
	"math"
	"strconv"
)

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

// DecodeResult 从原始 JSON 中解码并进行全量结构与业务校验。
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

	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, ErrMalformedDecision
	}

	if res.Model != model {
		return Result{}, ErrMalformedDecision
	}

	if err := ValidateResult(questions, res); err != nil {
		return Result{}, err
	}

	return res, nil
}
