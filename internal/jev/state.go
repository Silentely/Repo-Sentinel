package jev

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// MaxStateSize 是状态序列化后的最大字节上界（8KiB）。
const MaxStateSize = 8192

// FileFact 描述 PR 文件变更的静态事实。
type FileFact struct {
	Path          string `json:"path"`
	Documentation bool   `json:"documentation,omitempty"`
	Sensitive     bool   `json:"sensitive,omitempty"`
	Ambiguous     bool   `json:"ambiguous,omitempty"`
	Additions     int    `json:"additions,omitempty"`
	Deletions     int    `json:"deletions,omitempty"`
}

// GuardrailFacts 描述 ChatOps 动作护栏的事实上下文。
type GuardrailFacts struct {
	TargetState   string   `json:"target_state,omitempty"`
	IsDraft       bool     `json:"is_draft,omitempty"`
	ActorVerified bool     `json:"actor_verified"`
	Labels        []string `json:"labels,omitempty"`
	Text          string   `json:"text,omitempty"`
}

type notificationStatePayload struct {
	Repo               string   `json:"repo"`
	Kind               string   `json:"kind"`
	Action             string   `json:"action,omitempty"`
	SenderIsBot        bool     `json:"sender_is_bot"`
	WorkflowConclusion string   `json:"workflow_conclusion,omitempty"`
	Severity           string   `json:"severity,omitempty"`
	VerifiedLabels     []string `json:"verified_labels,omitempty"`
}

// BuildNotificationState 构建符合严格白名单的中性通知决策状态。
// 不读取任何未受信的 PayloadSummary 字段，不包含敏感凭据、URL、ID 或时间戳。
func BuildNotificationState(ev store.Event, repo string) (json.RawMessage, error) {
	p := notificationStatePayload{
		Repo:               strings.TrimSpace(repo),
		Kind:               strings.TrimSpace(ev.Kind),
		Action:             strings.TrimSpace(ev.Action),
		SenderIsBot:        ev.SenderIsBot,
		WorkflowConclusion: strings.TrimSpace(ev.WorkflowConclusion),
		Severity:           strings.TrimSpace(ev.Severity),
	}

	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxStateSize {
		return nil, ErrStateTooLarge
	}
	return json.RawMessage(raw), nil
}

type cascadeStatePayload struct {
	Files []FileFact `json:"files"`
}

// BuildCascadeState 构建 PR 级联门控决策状态。
func BuildCascadeState(files []FileFact) (json.RawMessage, error) {
	if files == nil {
		files = []FileFact{}
	}
	raw, err := json.Marshal(cascadeStatePayload{Files: files})
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxStateSize {
		return nil, ErrStateTooLarge
	}
	return json.RawMessage(raw), nil
}

type guardrailStatePayload struct {
	Action        string   `json:"action"`
	TargetState   string   `json:"target_state,omitempty"`
	IsDraft       bool     `json:"is_draft,omitempty"`
	ActorVerified bool     `json:"actor_verified"`
	Labels        []string `json:"labels,omitempty"`
	Text          string   `json:"text,omitempty"`
}

// BuildGuardrailState 构建 ChatOps 护栏决策状态。
func BuildGuardrailState(action string, facts GuardrailFacts) (json.RawMessage, error) {
	labels := facts.Labels
	if len(labels) > 0 {
		seen := make(map[string]bool)
		var cleaned []string
		for _, l := range labels {
			t := strings.TrimSpace(l)
			if t != "" && !seen[t] {
				seen[t] = true
				cleaned = append(cleaned, t)
			}
		}
		sort.Strings(cleaned)
		labels = cleaned
	}

	p := guardrailStatePayload{
		Action:        strings.TrimSpace(action),
		TargetState:   strings.TrimSpace(facts.TargetState),
		IsDraft:       facts.IsDraft,
		ActorVerified: facts.ActorVerified,
		Labels:        labels,
		Text:          strings.TrimSpace(facts.Text),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxStateSize {
		return nil, ErrStateTooLarge
	}
	return json.RawMessage(raw), nil
}
