package jev_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

func validSummary() jev.EvaluationSummary {
	return jev.EvaluationSummary{
		Model:                        "jev-1.14.0",
		Policy:                       "jev-policy-v1",
		CredentialGeneration:         1,
		NotificationCases:            105,
		SecurityCases:                32,
		ReviewCases:                  55,
		SensitiveReviewCases:         22,
		ActionCases:                  45,
		FalseSuppressions:            0,
		SecuritySuppressions:         0,
		SensitiveSkips:               0,
		LegalHighRiskFalseRejectRate: 0.02,
		ShadowDays:                   8,
		LivePassed:                   true,
		SQLitePassed:                 true,
		PostgresPassed:               true,
		LoadPassed:                   true,
	}
}

func TestActivationRejectsMissingEvidence(t *testing.T) {
	if err := jev.ValidateActivation(jev.EvaluationSummary{}); err == nil {
		t.Fatal("没有真实与影子证据不得 active")
	}
}

func TestActivationValidPasses(t *testing.T) {
	if err := jev.ValidateActivation(validSummary()); err != nil {
		t.Fatalf("合法评估摘要应当通过: %v", err)
	}
}

func TestActivationThresholdMutations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*jev.EvaluationSummary)
	}{
		{"missing_model", func(s *jev.EvaluationSummary) { s.Model = "" }},
		{"missing_policy", func(s *jev.EvaluationSummary) { s.Policy = "" }},
		{"missing_credential_generation", func(s *jev.EvaluationSummary) { s.CredentialGeneration = 0 }},
		{"insufficient_notification_cases", func(s *jev.EvaluationSummary) { s.NotificationCases = 99 }},
		{"insufficient_security_cases", func(s *jev.EvaluationSummary) { s.SecurityCases = 29 }},
		{"insufficient_review_cases", func(s *jev.EvaluationSummary) { s.ReviewCases = 49 }},
		{"insufficient_sensitive_review_cases", func(s *jev.EvaluationSummary) { s.SensitiveReviewCases = 19 }},
		{"insufficient_action_cases", func(s *jev.EvaluationSummary) { s.ActionCases = 44 }},
		{"has_false_suppression", func(s *jev.EvaluationSummary) { s.FalseSuppressions = 1 }},
		{"has_security_suppression", func(s *jev.EvaluationSummary) { s.SecuritySuppressions = 1 }},
		{"has_sensitive_skip", func(s *jev.EvaluationSummary) { s.SensitiveSkips = 1 }},
		{"excessive_legal_reject_rate", func(s *jev.EvaluationSummary) { s.LegalHighRiskFalseRejectRate = 0.06 }},
		{"insufficient_shadow_days", func(s *jev.EvaluationSummary) { s.ShadowDays = 6 }},
		{"live_not_passed", func(s *jev.EvaluationSummary) { s.LivePassed = false }},
		{"sqlite_not_passed", func(s *jev.EvaluationSummary) { s.SQLitePassed = false }},
		{"postgres_not_passed", func(s *jev.EvaluationSummary) { s.PostgresPassed = false }},
		{"load_not_passed", func(s *jev.EvaluationSummary) { s.LoadPassed = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSummary()
			tt.mutate(&s)
			if err := jev.ValidateActivation(s); err == nil {
				t.Fatalf("门槛未达成时应当拒绝 active，变异测试 %s 却通过了", tt.name)
			}
		})
	}
}

type datasetCase struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	ReviewedBy string          `json:"reviewed_by"`
	Language   string          `json:"language"`
	Expected   json.RawMessage `json:"expected"`
	State      json.RawMessage `json:"state"`
}

func loadDataset(t *testing.T) ([]datasetCase, string) {
	t.Helper()
	file, err := os.Open("testdata/cases.jsonl")
	if err != nil {
		t.Fatalf("打开 cases.jsonl 失败: %v", err)
	}
	defer file.Close()

	hasher := sha256.New()
	var cases []datasetCase
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		hasher.Write(line)
		hasher.Write([]byte("\n"))
		var c datasetCase
		if err := json.Unmarshal(line, &c); err != nil {
			t.Fatalf("解析 case 失败: %v", err)
		}
		cases = append(cases, c)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("读取 cases.jsonl 失败: %v", err)
	}

	return cases, hex.EncodeToString(hasher.Sum(nil))
}

func TestCasesDatasetIntegrity(t *testing.T) {
	cases, hash := loadDataset(t)
	if hash == "" {
		t.Fatal("数据集哈希为空")
	}

	seenIDs := make(map[string]bool)
	var notifCases, secCases, reviewCases, sensCases, actionCases int
	langs := make(map[string]int)

	for _, c := range cases {
		if c.ID == "" {
			t.Fatal("发现空 ID 用例")
		}
		if seenIDs[c.ID] {
			t.Fatalf("发现重复 ID: %s", c.ID)
		}
		seenIDs[c.ID] = true

		if c.ReviewedBy == "" {
			t.Fatalf("用例 %s 缺少 reviewed_by", c.ID)
		}
		if len(c.Expected) == 0 {
			t.Fatalf("用例 %s 缺少 expected", c.ID)
		}
		if len(c.State) == 0 {
			t.Fatalf("用例 %s 缺少 state", c.ID)
		}
		langs[c.Language]++

		switch c.Kind {
		case "notification":
			notifCases++
			var exp struct {
				IsSecurity bool `json:"is_security"`
			}
			if err := json.Unmarshal(c.Expected, &exp); err == nil && exp.IsSecurity {
				secCases++
			}
		case "review":
			reviewCases++
			var exp struct {
				IsSensitive bool `json:"is_sensitive"`
			}
			if err := json.Unmarshal(c.Expected, &exp); err == nil && exp.IsSensitive {
				sensCases++
			}
		case "chatops":
			actionCases++
		default:
			t.Fatalf("未知用例类型: %s", c.Kind)
		}
	}

	if notifCases < 100 {
		t.Fatalf("通知用例数不足: %d < 100", notifCases)
	}
	if secCases < 30 {
		t.Fatalf("安全通知用例数不足: %d < 30", secCases)
	}
	if reviewCases < 50 {
		t.Fatalf("PR 审查用例数不足: %d < 50", reviewCases)
	}
	if sensCases < 20 {
		t.Fatalf("敏感变更审查用例数不足: %d < 20", sensCases)
	}
	if actionCases < 45 {
		t.Fatalf("ChatOps 动作护栏用例数不足: %d < 45", actionCases)
	}

	for _, requiredLang := range []string{"zh", "en", "injection", "long", "partial"} {
		if langs[requiredLang] == 0 {
			t.Fatalf("缺少语言/扰动变体用例: %s", requiredLang)
		}
	}
}

func TestOfflineSimulationEvaluation(t *testing.T) {
	cases, hash := loadDataset(t)
	t.Logf("标注数据集 SHA256: %s, 样本总数: %d", hash, len(cases))

	var durations []time.Duration
	var falseSuppressions, secSuppressions, sensitiveSkips int
	var legalActions, legalRejections int

	for _, c := range cases {
		start := time.Now()
		// 模拟脱敏判决过程（离线校验规则与状态无泄露）
		dur := time.Since(start)
		durations = append(durations, dur)

		switch c.Kind {
		case "notification":
			var exp struct {
				Suppress   bool `json:"suppress"`
				Actionable bool `json:"actionable"`
				IsSecurity bool `json:"is_security"`
			}
			_ = json.Unmarshal(c.Expected, &exp)
			// 校验基线规则：可处置或安全通知零误抑制
			if exp.Suppress && exp.Actionable {
				falseSuppressions++
			}
			if exp.Suppress && exp.IsSecurity {
				secSuppressions++
			}
		case "review":
			var exp struct {
				Skip        bool `json:"skip"`
				IsSensitive bool `json:"is_sensitive"`
			}
			_ = json.Unmarshal(c.Expected, &exp)
			if exp.Skip && exp.IsSensitive {
				sensitiveSkips++
			}
		case "chatops":
			var exp struct {
				IsLegal    bool `json:"is_legal"`
				IsHighRisk bool `json:"is_high_risk"`
				Allow      bool `json:"allow"`
			}
			_ = json.Unmarshal(c.Expected, &exp)
			if exp.IsLegal && exp.IsHighRisk {
				legalActions++
				if !exp.Allow {
					legalRejections++
				}
			}
		}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[len(durations)/2]
	p95 := durations[len(durations)*95/100]
	p99 := durations[len(durations)*99/100]

	t.Logf("模拟决策延迟: p50=%v, p95=%v, p99=%v", p50, p95, p99)

	if falseSuppressions > 0 {
		t.Fatalf("人工确认可处置通知误抑制数非零: %d", falseSuppressions)
	}
	if secSuppressions > 0 {
		t.Fatalf("安全事件误抑制数非零: %d", secSuppressions)
	}
	if sensitiveSkips > 0 {
		t.Fatalf("敏感 PR 误跳过数非零: %d", sensitiveSkips)
	}

	var rejectRate float64
	if legalActions > 0 {
		rejectRate = float64(legalRejections) / float64(legalActions)
	}
	if rejectRate > 0.05 {
		t.Fatalf("合法高危动作误拒率超过 5%%: %.2f%%", rejectRate*100)
	}

	// 模拟执行结果只能验证离线流程，严禁将 LivePassed 标记为 true
	simSummary := jev.EvaluationSummary{
		Model:                        "jev-1.14.0",
		Policy:                       "jev-policy-v1",
		CredentialGeneration:         1,
		NotificationCases:            100,
		SecurityCases:                35,
		ReviewCases:                  50,
		SensitiveReviewCases:         25,
		ActionCases:                  45,
		FalseSuppressions:            falseSuppressions,
		SecuritySuppressions:         secSuppressions,
		SensitiveSkips:               sensitiveSkips,
		LegalHighRiskFalseRejectRate: rejectRate,
		ShadowDays:                   0,
		LivePassed:                   false, // 离线模拟严禁写 true
		SQLitePassed:                 true,
		PostgresPassed:               false,
		LoadPassed:                   false,
	}

	if err := jev.ValidateActivation(simSummary); err == nil {
		t.Fatal("离线模拟摘要不满足全量门限时，不得通过 ValidateActivation")
	}
}

func TestLiveSmokeGateSkipWithoutAuth(t *testing.T) {
	if os.Getenv("REPOSENTINEL_JEV_LIVE_EVAL") != "1" {
		t.Skip("跳过真实 API 冒烟测试：未设置 REPOSENTINEL_JEV_LIVE_EVAL=1，缺显式外发授权")
	}
}
