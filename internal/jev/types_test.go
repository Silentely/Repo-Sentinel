package jev

import (
	"errors"
	"math"
	"os"
	"testing"
)

func TestNoulPresence(t *testing.T) {
	q := map[string]Question{"gate": NoulQuestion{Type: "noul", Instructions: "Is action required?"}}
	valid := []byte(`{"model":"jev-1.14.0","answers":{"gate":{"type":"noul","noul":0}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	if _, err := DecodeResult(valid, q, "jev-1.14.0"); err != nil {
		t.Fatal(err)
	}
	missing := []byte(`{"model":"jev-1.14.0","answers":{"gate":{"type":"noul"}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	if _, err := DecodeResult(missing, q, "jev-1.14.0"); !errors.Is(err, ErrMalformedDecision) {
		t.Fatalf("缺 noul 必须整体拒绝: %v", err)
	}
	nullNoul := []byte(`{"model":"jev-1.14.0","answers":{"gate":{"type":"noul","noul":null}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	if _, err := DecodeResult(nullNoul, q, "jev-1.14.0"); !errors.Is(err, ErrMalformedDecision) {
		t.Fatalf("null noul 必须整体拒绝: %v", err)
	}
}

func TestScorePrecisionAndLegality(t *testing.T) {
	q := map[string]Question{
		"urgency": ScoreQuestion{
			Type:         "score",
			Instructions: "Rate urgency",
			Criteria:     []string{"Low", "Medium", "High"},
		},
	}
	valid := []byte(`{
		"model": "jev-1.14.0",
		"answers": {
			"urgency": {
				"type": "score",
				"score": 1.6,
				"confidence": 0.88,
				"legend": {"0": "Low", "1": "Medium", "2": "High"},
				"probabilities": {"0": 0.1, "1": 0.2, "2": 0.7}
			}
		},
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)
	if _, err := DecodeResult(valid, q, "jev-1.14.0"); err != nil {
		t.Fatalf("合法 score 响应解析失败: %v", err)
	}

	invalidScore := []byte(`{
		"model": "jev-1.14.0",
		"answers": {
			"urgency": {
				"type": "score",
				"score": 1.5,
				"confidence": 0.88,
				"legend": {"0": "Low", "1": "Medium", "2": "High"},
				"probabilities": {"0": 0.1, "1": 0.2, "2": 0.7}
			}
		},
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)
	if _, err := DecodeResult(invalidScore, q, "jev-1.14.0"); !errors.Is(err, ErrMalformedDecision) {
		t.Fatalf("加权和不匹配必须整体拒绝: %v", err)
	}
}

func TestDecodeFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/systemone.response.json")
	if err != nil {
		t.Fatal(err)
	}
	q := map[string]Question{
		"is_actionable": NoulQuestion{Type: "noul", Instructions: "Does this event require human intervention?"},
		"severity_tier": ChoiceQuestion{Type: "choice", Instructions: "Select severity", Criteria: map[string]*string{"critical": nil, "high": nil, "medium": nil, "low": nil}},
		"urgency":       ScoreQuestion{Type: "score", Instructions: "Rate urgency", Criteria: []string{"Routine", "Soon", "Immediate"}},
	}
	res, err := DecodeResult(raw, q, "jev-1.14.0")
	if err != nil {
		t.Fatalf("官方测试响应反序列化校验失败: %v", err)
	}
	if res.Model != "jev-1.14.0" {
		t.Fatalf("unexpected model %s", res.Model)
	}
}

func TestMalformedMatrix(t *testing.T) {
	qChoice := map[string]Question{
		"tier": ChoiceQuestion{
			Type:         "choice",
			Instructions: "Pick tier",
			Criteria:     map[string]*string{"a": nil, "b": nil},
		},
	}

	cases := []struct {
		name string
		json string
	}{
		{"missing_answers", `{"model":"jev-1.14.0","usage":{"input_tokens":1,"output_tokens":1}}`},
		{"missing_usage", `{"model":"jev-1.14.0","answers":{}}`},
		{"missing_model", `{"answers":{},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"model_mismatch", `{"model":"other-model","answers":{"tier":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.9,"b":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"wrong_type", `{"model":"jev-1.14.0","answers":{"tier":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"unknown_choice", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"c","confidence":0.9,"probabilities":{"a":0.9,"b":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"missing_prob_key", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":1.0}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"prob_sum_not_one", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.5,"b":0.4}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"choice_not_max", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.3,"b":0.7}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"missing_confidence", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","probabilities":{"a":0.9,"b":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"confidence_out_of_bounds", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","confidence":1.2,"probabilities":{"a":0.9,"b":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"negative_tokens", `{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"a","confidence":0.9,"probabilities":{"a":0.9,"b":0.1}}},"usage":{"input_tokens":-1,"output_tokens":1}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeResult([]byte(tc.json), qChoice, "jev-1.14.0"); !errors.Is(err, ErrMalformedDecision) {
				t.Fatalf("[%s] 应拒绝并返回 ErrMalformedDecision，得到: %v", tc.name, err)
			}
		})
	}
}

func TestNaNAndInfRejection(t *testing.T) {
	q := map[string]Question{"gate": NoulQuestion{Type: "noul", Instructions: "Check"}}
	nanVal := math.NaN()
	infVal := math.Inf(1)

	res := Result{
		Model: "jev-1.14.0",
		Usage: Usage{InputTokens: 1, OutputTokens: 1},
		Answers: map[string]Answer{
			"gate": {
				Type: "noul",
				Noul: &nanVal,
			},
		},
	}
	if err := ValidateResult(q, res); !errors.Is(err, ErrMalformedDecision) {
		t.Fatalf("NaN 必须拒绝: %v", err)
	}

	res.Answers["gate"] = Answer{Type: "noul", Noul: &infVal}
	if err := ValidateResult(q, res); !errors.Is(err, ErrMalformedDecision) {
		t.Fatalf("Inf 必须拒绝: %v", err)
	}
}
