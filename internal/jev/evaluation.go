package jev

import (
	"errors"
	"fmt"
)

const (
	// EvaluationSettingKey 是 system_settings 中 Jev 上线评估摘要的键。
	EvaluationSettingKey = "jev.evaluation.v1"
)

// EvaluationSummary 记录上线门禁所需的离线标注、线上冒烟、影子运行与数据库兼容性证据。
type EvaluationSummary struct {
	Model                        string  `json:"model"`
	Policy                       string  `json:"policy"`
	CredentialGeneration         uint64  `json:"credential_generation"`
	NotificationCases            int     `json:"notification_cases"`
	SecurityCases                int     `json:"security_cases"`
	ReviewCases                  int     `json:"review_cases"`
	SensitiveReviewCases         int     `json:"sensitive_review_cases"`
	ActionCases                  int     `json:"action_cases"`
	FalseSuppressions            int     `json:"false_suppressions"`
	SecuritySuppressions         int     `json:"security_suppressions"`
	SensitiveSkips               int     `json:"sensitive_skips"`
	LegalHighRiskFalseRejectRate float64 `json:"legal_high_risk_false_reject_rate"`
	ShadowDays                   int     `json:"shadow_days"`
	LivePassed                   bool    `json:"live_passed"`
	SQLitePassed                 bool    `json:"sqlite_passed"`
	PostgresPassed               bool    `json:"postgres_passed"`
	LoadPassed                   bool    `json:"load_passed"`
}

// ValidateActivation 严格依照设计规范 §9 门槛验证是否允许切换至 active 生产模式。
func ValidateActivation(summary EvaluationSummary) error {
	if summary.Model == "" {
		return errors.New("missing evaluated model name")
	}
	if summary.Policy == "" {
		return errors.New("missing evaluated policy name")
	}
	if summary.CredentialGeneration == 0 {
		return errors.New("missing valid credential generation")
	}
	if summary.NotificationCases < 100 {
		return fmt.Errorf("insufficient notification cases: got %d, required >= 100", summary.NotificationCases)
	}
	if summary.SecurityCases < 30 {
		return fmt.Errorf("insufficient security notification cases: got %d, required >= 30", summary.SecurityCases)
	}
	if summary.ReviewCases < 50 {
		return fmt.Errorf("insufficient review cases: got %d, required >= 50", summary.ReviewCases)
	}
	if summary.SensitiveReviewCases < 20 {
		return fmt.Errorf("insufficient sensitive review cases: got %d, required >= 20", summary.SensitiveReviewCases)
	}
	if summary.ActionCases < 45 {
		return fmt.Errorf("insufficient action cases: got %d, required >= 45 (5 per action across 9 actions)", summary.ActionCases)
	}
	if summary.FalseSuppressions > 0 {
		return fmt.Errorf("actionable notification false suppressions must be 0: got %d", summary.FalseSuppressions)
	}
	if summary.SecuritySuppressions > 0 {
		return fmt.Errorf("security event false suppressions must be 0: got %d", summary.SecuritySuppressions)
	}
	if summary.SensitiveSkips > 0 {
		return fmt.Errorf("sensitive PR false skips must be 0: got %d", summary.SensitiveSkips)
	}
	if summary.LegalHighRiskFalseRejectRate > 0.05 {
		return fmt.Errorf("legal action false rejection rate exceeds 5%% threshold: got %.2f%%", summary.LegalHighRiskFalseRejectRate*100)
	}
	if summary.ShadowDays < 7 {
		return fmt.Errorf("insufficient shadow evaluation period: got %d days, required >= 7 days", summary.ShadowDays)
	}
	if !summary.LivePassed {
		return errors.New("live real API smoke tests not passed or missing credentials")
	}
	if !summary.SQLitePassed {
		return errors.New("sqlite dialect tests not passed")
	}
	if !summary.PostgresPassed {
		return errors.New("postgres dialect tests not passed")
	}
	if !summary.LoadPassed {
		return errors.New("high concurrency load & fault injection tests not passed")
	}

	return nil
}
