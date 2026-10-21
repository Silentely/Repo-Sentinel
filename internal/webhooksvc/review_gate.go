package webhooksvc

import (
	"context"
	"errors"

	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

// shouldSkipByJev 向 Jev 判别式引擎咨询 PR 变更是否可安全跳过深度代码审查。
// 仅在本地门控为 "consult" 且前置安全检查通过时调用。
// 必须同时满足：
// 1. needs_deep_review <= 0.10
// 2. risk_surface == "documentation_only" 且 confidence >= 0.90
// 否则返回 false（不跳过，走原 LLM 深度审查）。
func shouldSkipByJev(ctx context.Context, client jev.Client, files []jev.FileFact) (bool, error) {
	if client == nil {
		return false, errors.New("jev client is nil")
	}

	stateRaw, err := jev.BuildCascadeState(files)
	if err != nil {
		return false, err
	}

	questions := map[string]jev.Question{
		"needs_deep_review": jev.NoulQuestion{
			Type:         "noul",
			Instructions: "Assess whether this PR needs deep code review based only on the provided file facts.",
		},
		"risk_surface": jev.ChoiceQuestion{
			Type:         "choice",
			Instructions: "Classify the risk surface of the file changes.",
			Criteria: map[string]*string{
				"documentation_only": nil,
				"business_logic":     nil,
				"security_critical":  nil,
			},
		},
	}

	resp, err := client.Judge(ctx, stateRaw, questions)
	if err != nil {
		return false, err
	}

	ansNeeds, okNeeds := resp.Answers["needs_deep_review"]
	ansRisk, okRisk := resp.Answers["risk_surface"]
	if !okNeeds || !okRisk {
		return false, nil
	}

	if ansNeeds.Noul == nil || *ansNeeds.Noul > 0.10 {
		return false, nil
	}

	if ansRisk.Choice == nil || *ansRisk.Choice != "documentation_only" {
		return false, nil
	}

	if ansRisk.Confidence == nil || *ansRisk.Confidence < 0.90 {
		return false, nil
	}

	return true, nil
}
