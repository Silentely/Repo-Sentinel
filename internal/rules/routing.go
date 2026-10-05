package rules

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// MatchChannelFilter 多维评估渠道规则：仓库通配符、分支通配符与严重度门槛。
func MatchChannelFilter(ch store.NotificationChannel, ev *store.Event, repoFullName string, branch string) bool {
	// 1. 仓库通配符匹配（否定规则优先）
	if !matchPatternList(ch.RepoPattern, repoFullName, true) {
		return false
	}

	// 2. 分支通配符匹配（事件无分支或规则为空时放行）
	if branch != "" && strings.TrimSpace(ch.BranchFilter) != "" {
		if !matchPatternList(ch.BranchFilter, branch, false) {
			return false
		}
	}

	// 3. 严重度门槛评估
	if ev != nil && strings.TrimSpace(ev.Severity) != "" {
		minSev := ch.MinSeverity
		if strings.TrimSpace(minSev) == "" {
			minSev = "low"
		}
		if store.SeverityWeight(ev.Severity) < store.SeverityWeight(minSev) {
			return false
		}
	}

	return true
}

// matchPatternList 评估逗号分隔的通配符模式列表，遵循否定模式最高优先级原则。
// 当 allowEmptyPass 为 true 且 pattern 为空时放行。
func matchPatternList(patternStr, target string, allowEmptyPass bool) bool {
	patternStr = strings.TrimSpace(patternStr)
	if patternStr == "" {
		return allowEmptyPass
	}

	tokens := strings.Split(patternStr, ",")
	var posPatterns []string
	var negPatterns []string

	for _, token := range tokens {
		t := strings.TrimSpace(token)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "!") {
			neg := strings.TrimSpace(t[1:])
			if neg != "" {
				negPatterns = append(negPatterns, neg)
			}
		} else {
			posPatterns = append(posPatterns, t)
		}
	}

	// 1. 否定规则最高优先级：只要命中任何一个否定规则，立即拒绝
	for _, neg := range negPatterns {
		if globMatch(neg, target) {
			return false
		}
	}

	// 2. 如果存在肯定规则，必须命中至少一个肯定规则
	if len(posPatterns) > 0 {
		for _, pos := range posPatterns {
			if globMatch(pos, target) {
				return true
			}
		}
		return false
	}

	// 3. 如果只有否定规则且全部未命中，则放行
	return true
}

// globMatch 执行通配符匹配，支持 * 与 ?，不区分大小写，且 * 允许匹配斜杠。
func globMatch(pattern, text string) bool {
	pattern = strings.TrimSpace(pattern)
	var sb strings.Builder
	sb.WriteString("(?i)^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}
	return re.MatchString(text)
}

// mutePayload 表示紧急静音设置的内容。
type mutePayload struct {
	Muted     bool      `json:"muted"`
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CheckEmergencyMute 检查全局或仓库级紧急静音。
func CheckEmergencyMute(ctx context.Context, st store.Store, repoID string) (bool, string) {
	if st == nil {
		return false, ""
	}

	keys := []string{store.MuteAllSettingKey()}
	if repoID != "" {
		keys = append(keys, store.MuteSettingKey(repoID))
	}

	settings, err := st.Settings().GetMany(ctx, keys...)
	if err != nil || len(settings) == 0 {
		return false, ""
	}

	now := time.Now().UTC()
	for _, s := range settings {
		var p mutePayload
		if err := json.Unmarshal(s.ValueJSON, &p); err != nil {
			continue
		}
		if p.Muted && (p.ExpiresAt.IsZero() || p.ExpiresAt.After(now)) {
			reason := p.Reason
			if reason == "" {
				reason = "emergency_mute"
			}
			return true, reason
		}
	}

	return false, ""
}

// ExtractEventBranch 从规范化事件负载中提取目标分支。
func ExtractEventBranch(ev *store.Event) string {
	if ev == nil || ev.PayloadSummary == nil {
		return ""
	}
	if b, ok := ev.PayloadSummary["branch"].(string); ok && b != "" {
		return b
	}
	if b, ok := ev.PayloadSummary["head_branch"].(string); ok && b != "" {
		return b
	}
	if b, ok := ev.PayloadSummary["base_ref"].(string); ok && b != "" {
		return b
	}
	if ref, ok := ev.PayloadSummary["ref"].(string); ok && ref != "" {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ""
}
