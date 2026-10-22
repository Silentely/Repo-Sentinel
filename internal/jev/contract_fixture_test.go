package jev

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestContractFixture(t *testing.T) {
	for _, name := range []string{"systemone.request.json", "systemone.response.json"} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatal(err)
		}
		keys := []string{"model", "state", "questions"}
		if name == "systemone.response.json" {
			keys = []string{"model", "answers", "usage"}
		}
		for _, key := range keys {
			if len(top[key]) == 0 || string(top[key]) == "null" {
				t.Fatalf("缺字段 %s", key)
			}
		}
	}
}

func TestResponseSelfCheck(t *testing.T) {
	// 读取脱敏真实 Jev 响应多场景样本
	raw, err := os.ReadFile("testdata/systemone.real_sample.json")
	if err != nil {
		t.Fatalf("read real_sample.json failed: %v", err)
	}

	var samples map[string]json.RawMessage
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("unmarshal samples failed: %v", err)
	}

	// 1. 验证通知分诊场景
	t.Run("NotificationTriage", func(t *testing.T) {
		sampleJSON, ok := samples["notification_triage"]
		if !ok {
			t.Fatal("missing notification_triage sample")
		}
		q := map[string]Question{
			"is_actionable": NoulQuestion{Type: "noul", Instructions: "Action required?"},
			"noise_type": ChoiceQuestion{
				Type:         "choice",
				Instructions: "Noise type",
				Criteria:     map[string]*string{"normal": nil, "bot_chatter": nil},
			},
			"severity_tier": ChoiceQuestion{
				Type:         "choice",
				Instructions: "Severity tier",
				Criteria:     map[string]*string{"critical": nil, "high": nil, "medium": nil, "low": nil},
			},
			"incident_tag": ChoiceQuestion{
				Type:         "choice",
				Instructions: "Incident tag",
				Criteria:     map[string]*string{"security": nil, "ci": nil, "dependency": nil, "maintenance": nil, "general": nil},
			},
		}
		res, err := DecodeResult(sampleJSON, q, "jev-1.14.0")
		if err != nil {
			t.Fatalf("DecodeResult failed on notification_triage: %v", err)
		}
		if res.Usage.InputTokens <= 0 || res.Usage.OutputTokens <= 0 {
			t.Errorf("expected positive usage, got %+v", res.Usage)
		}
	})

	// 2. 验证 PR 级联放行场景
	t.Run("ReviewCascade", func(t *testing.T) {
		sampleJSON, ok := samples["review_cascade"]
		if !ok {
			t.Fatal("missing review_cascade sample")
		}
		q := map[string]Question{
			"needs_deep_review": NoulQuestion{Type: "noul", Instructions: "Needs deep review?"},
			"risk_surface": ChoiceQuestion{
				Type:         "choice",
				Instructions: "Risk surface",
				Criteria:     map[string]*string{"documentation_only": nil, "business_logic": nil, "security_critical": nil},
			},
		}
		res, err := DecodeResult(sampleJSON, q, "jev-1.14.0")
		if err != nil {
			t.Fatalf("DecodeResult failed on review_cascade: %v", err)
		}
		if *res.Answers["needs_deep_review"].Noul > 0.10 {
			t.Errorf("expected low needs_deep_review, got %f", *res.Answers["needs_deep_review"].Noul)
		}
	})

	// 3. 验证 ChatOps 护栏场景
	t.Run("ChatOpsGuardrail", func(t *testing.T) {
		sampleJSON, ok := samples["chatops_guardrail"]
		if !ok {
			t.Fatal("missing chatops_guardrail sample")
		}
		q := map[string]Question{
			"safety_decision": ChoiceQuestion{
				Type:         "choice",
				Instructions: "Safety decision",
				Criteria:     map[string]*string{"allow": nil, "deny": nil, "uncertain": nil},
			},
		}
		res, err := DecodeResult(sampleJSON, q, "jev-1.14.0")
		if err != nil {
			t.Fatalf("DecodeResult failed on chatops_guardrail: %v", err)
		}
		if *res.Answers["safety_decision"].Choice != "allow" {
			t.Errorf("expected allow, got %s", *res.Answers["safety_decision"].Choice)
		}
	})

	// 4. 验证评分度量场景
	t.Run("ScoreMetric", func(t *testing.T) {
		sampleJSON, ok := samples["score_metric"]
		if !ok {
			t.Fatal("missing score_metric sample")
		}
		q := map[string]Question{
			"urgency": ScoreQuestion{
				Type:         "score",
				Instructions: "Rate urgency",
				Criteria:     []string{"Routine", "Soon", "Immediate"},
			},
		}
		res, err := DecodeResult(sampleJSON, q, "jev-1.14.0")
		if err != nil {
			t.Fatalf("DecodeResult failed on score_metric: %v", err)
		}
		if *res.Answers["urgency"].Score != 1.7 {
			t.Errorf("expected score 1.7, got %f", *res.Answers["urgency"].Score)
		}
	})
}

func TestValidateRequest_Strict(t *testing.T) {
	validRaw := []byte(`{
		"model": "jev-1.14.0",
		"state": {"kind": "workflow_run"},
		"questions": {
			"decision": {
				"type": "noul",
				"instructions": "Should proceed?"
			},
			"choice_q": {
				"type": "choice",
				"instructions": "Pick category",
				"criteria": {"a": null, "b": null}
			},
			"score_q": {
				"type": "score",
				"instructions": "Rate urgency",
				"criteria": ["low", "high"]
			}
		}
	}`)

	req, err := DecodeRequest(validRaw)
	if err != nil {
		t.Fatalf("DecodeRequest failed on valid raw: %v", err)
	}
	if req.Model != "jev-1.14.0" {
		t.Errorf("expected model jev-1.14.0, got %s", req.Model)
	}

	// 异常请求测试：缺 model、缺 state、state 为 null、缺 questions.decision.type、非法 type
	invalidCases := []struct {
		name string
		json string
	}{
		{"missing model", `{"state":{"k":1},"questions":{"d":{"type":"noul","instructions":"i"}}}`},
		{"missing state", `{"model":"m","questions":{"d":{"type":"noul","instructions":"i"}}}`},
		{"null state", `{"model":"m","state":null,"questions":{"d":{"type":"noul","instructions":"i"}}}`},
		{"missing question type", `{"model":"m","state":{"k":1},"questions":{"decision":{"instructions":"i"}}}`},
		{"invalid question type", `{"model":"m","state":{"k":1},"questions":{"decision":{"type":"free_text","instructions":"i"}}}`},
		{"choice criteria too few", `{"model":"m","state":{"k":1},"questions":{"decision":{"type":"choice","instructions":"i","criteria":{"only_one":null}}}}`},
		{"score criteria too few", `{"model":"m","state":{"k":1},"questions":{"decision":{"type":"score","instructions":"i","criteria":["only_one"]}}}`},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeRequest([]byte(tc.json))
			if !errors.Is(err, ErrMalformedDecision) {
				t.Fatalf("expected ErrMalformedDecision for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestDecodeResult_MissingFieldsFail(t *testing.T) {
	q := map[string]Question{
		"decision": NoulQuestion{Type: "noul", Instructions: "Should pass?"},
	}

	// 缺 usage.input_tokens、缺 answers、类型不符（noul 为字符串）等均必须判定为失败
	failingCases := []struct {
		name string
		json string
	}{
		{"missing usage", `{"model":"m","answers":{"decision":{"type":"noul","noul":0.5}}}`},
		{"empty usage", `{"model":"m","usage":{},"answers":{"decision":{"type":"noul","noul":0.5}}}`},
		{"negative input tokens", `{"model":"m","usage":{"input_tokens":-1,"output_tokens":10},"answers":{"decision":{"type":"noul","noul":0.5}}}`},
		{"missing answers", `{"model":"m","usage":{"input_tokens":10,"output_tokens":10}}`},
		{"missing question answer", `{"model":"m","usage":{"input_tokens":10,"output_tokens":10},"answers":{}}`},
		{"wrong answer type", `{"model":"m","usage":{"input_tokens":10,"output_tokens":10},"answers":{"decision":{"type":"choice","choice":"opt"}}}`},
		{"noul value is string", `{"model":"m","usage":{"input_tokens":10,"output_tokens":10},"answers":{"decision":{"type":"noul","noul":"0.5"}}}`},
		{"model mismatch", `{"model":"other-model","usage":{"input_tokens":10,"output_tokens":10},"answers":{"decision":{"type":"noul","noul":0.5}}}`},
	}

	for _, tc := range failingCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeResult([]byte(tc.json), q, "m")
			if !errors.Is(err, ErrMalformedDecision) {
				t.Fatalf("expected ErrMalformedDecision on %s, got %v", tc.name, err)
			}
		})
	}
}

func TestEvaluationCasesJSONL(t *testing.T) {
	file, err := os.Open("evaluation/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item map[string]json.RawMessage
		if err := json.Unmarshal(line, &item); err != nil {
			t.Fatalf("cases.jsonl 行 %d 解析失败: %v", count+1, err)
		}
		for _, req := range []string{"id", "plane", "language", "state", "expected", "reviewed_by"} {
			if len(item[req]) == 0 || string(item[req]) == "null" {
				t.Fatalf("cases.jsonl 行 %d 缺少必需字段 %s", count+1, req)
			}
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("cases.jsonl 不能为空")
	}
}
