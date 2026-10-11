package digest

import (
	htmlpkg "html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

var (
	reRepoName          = regexp.MustCompile(`\b([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+)\b`)
	reIssueNum          = regexp.MustCompile(`(#\d+)\b`)
	reVersionTransition = regexp.MustCompile(`(v?\d+\.\d+(?:\.\d+)?)\s*(?:→|->|➔|➜)\s*(v?\d+\.\d+(?:\.\d+)?)`)
	reVersion           = regexp.MustCompile(`\b(v\d+\.\d+(?:\.\d+)?(?:-[a-zA-Z0-9.]+)?)\b`)
	reSeverity          = regexp.MustCompile(`(?i)\b(critical|high|medium|low)\b`)
	reUrgentKeywords    = regexp.MustCompile(`(需尽快确认|尽快确认|需人工介入|紧急处理)`)
)

// buildDigestMetricsCard 基于实际事件集生成紧凑的指标统计速览卡。
func buildDigestMetricsCard(events []store.Event, period string) string {
	if len(events) == 0 {
		return ""
	}
	var (
		alerts     int
		highAlerts int
		ciFails    int
		releases   int
		issues     int
		depUpdates int
	)

	for _, ev := range events {
		switch ev.Kind {
		case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
			alerts++
			sev := strings.ToLower(ev.Severity)
			if sev == "high" || sev == "critical" {
				highAlerts++
			}
		case store.WorkflowRunKind:
			if ev.Action == "failure" || ev.Action == "cancelled" || ev.Action == "timed_out" {
				ciFails++
			}
		case store.ReleaseKind:
			releases++
		case store.WorkItemKindIssue:
			issues++
		}

		titleLower := strings.ToLower(ev.Title)
		if strings.Contains(titleLower, "renovate") ||
			strings.Contains(titleLower, "dependabot") ||
			strings.Contains(titleLower, "dependency") ||
			strings.Contains(titleLower, "update actions/") ||
			strings.Contains(titleLower, "update dependency") {
			depUpdates++
		}
	}

	var pulseBadge string
	if highAlerts > 0 || ciFails >= 3 {
		pulseBadge = " · 🚨 <b>[需紧急介入]</b>"
	} else if alerts > 0 || ciFails > 0 || issues >= 5 {
		pulseBadge = " · ⚠️ <b>[需重点关注]</b>"
	} else {
		pulseBadge = " · 🟢 <b>[健康平稳]</b>"
	}

	var sb strings.Builder
	sb.WriteString("📈 <b>")
	sb.WriteString(htmlpkg.EscapeString(period))
	sb.WriteString(" · 共 ")
	sb.WriteString(strconv.Itoa(len(events)))
	sb.WriteString(" 条事件</b>")
	sb.WriteString(pulseBadge)
	sb.WriteString("\n")

	var parts []string
	if highAlerts > 0 {
		parts = append(parts, "🚨 高危告警: "+strconv.Itoa(highAlerts))
	} else if alerts > 0 {
		parts = append(parts, "🛡️ 安全告警: "+strconv.Itoa(alerts))
	}
	if ciFails > 0 {
		parts = append(parts, "❌ CI失败: "+strconv.Itoa(ciFails))
	}
	if issues > 0 {
		parts = append(parts, "🐛 缺陷: "+strconv.Itoa(issues))
	}
	if releases > 0 {
		parts = append(parts, "🚀 发布: "+strconv.Itoa(releases))
	}
	if depUpdates > 0 {
		parts = append(parts, "📦 依赖更新: "+strconv.Itoa(depUpdates))
	}

	if len(parts) > 0 {
		sb.WriteString(strings.Join(parts, "  |  "))
		sb.WriteString("\n")
	}
	sb.WriteString("────────────────\n")
	return sb.String()
}

// formatDigestSummaryHTML 将 AI 生成的自然语言摘要转换为具备视觉层次与原生折叠块的 Telegram HTML。
func formatDigestSummaryHTML(summary string) string {
	rawLines := strings.Split(strings.TrimSpace(summary), "\n")
	if len(rawLines) == 0 {
		return ""
	}

	var (
		overviewLines []string
		priorityLines []string
		releaseLines  []string
		routineLines  []string
		currentBucket = "overview" // overview | priority | release | routine
	)

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// 检查版块标题
		if strings.Contains(trimmed, "【重点关注】") {
			currentBucket = "priority"
			continue
		} else if strings.Contains(trimmed, "【版本发布】") {
			currentBucket = "release"
			continue
		} else if strings.Contains(trimmed, "【常规动态】") || strings.Contains(trimmed, "【常规更新】") {
			currentBucket = "routine"
			continue
		}

		isBullet := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "• ")
		text := trimmed
		if isBullet {
			text = strings.TrimSpace(trimmed[2:])
		}

		// 如果未明确处于版块中，通过前缀关键词自动识别归类
		bucket := currentBucket
		if bucket == "overview" && isBullet {
			lower := strings.ToLower(text)
			if strings.Contains(text, "安全告警") || strings.Contains(text, "漏洞") ||
				strings.Contains(text, "CI") || strings.Contains(text, "失败") ||
				strings.Contains(text, "Bug") || strings.Contains(text, "缺陷") ||
				strings.Contains(text, "问题跟踪") || strings.Contains(text, "故障") {
				bucket = "priority"
			} else if strings.Contains(text, "版本发布") || strings.Contains(text, "发布") {
				bucket = "release"
			} else if strings.Contains(text, "依赖更新") || strings.Contains(text, "Renovate") ||
				strings.Contains(text, "Dependabot") || strings.Contains(text, "配置") ||
				strings.Contains(text, "仪表盘") || strings.Contains(text, "未见 CI 失败") {
				bucket = "routine"
			} else {
				bucket = "priority"
			}
		}

		formatted := highlightEntities(text)

		switch bucket {
		case "overview":
			if isBullet {
				overviewLines = append(overviewLines, "• "+formatted)
			} else {
				overviewLines = append(overviewLines, formatted)
			}
		case "priority":
			priorityLines = append(priorityLines, formatPriorityItem(formatted))
		case "release":
			releaseLines = append(releaseLines, formatReleaseItem(formatted))
		case "routine":
			routineLines = append(routineLines, formatRoutineItem(formatted))
		}
	}

	var sb strings.Builder

	// 1. 概述段落
	if len(overviewLines) > 0 {
		sb.WriteString(strings.Join(overviewLines, "\n"))
		sb.WriteString("\n")
	}

	// 2. 重点关注项（永不折叠，置顶醒目显示）
	if len(priorityLines) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Join(priorityLines, "\n"))
		sb.WriteString("\n")
	}

	// 3. 版本发布项
	if len(releaseLines) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Join(releaseLines, "\n"))
		sb.WriteString("\n")
	}

	// 4. 常规依赖更新与次要变更（放入 Telegram 原生可折叠块）
	if len(routineLines) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("<blockquote expandable>\n<b>📦 常规依赖更新与次要动态（点击展开）</b>\n")
		sb.WriteString(strings.Join(routineLines, "\n"))
		sb.WriteString("\n</blockquote>")
	}

	return strings.TrimSpace(sb.String())
}

func highlightEntities(text string) string {
	escaped := htmlpkg.EscapeString(text)

	// 仓库名 -> <code>owner/repo</code>
	escaped = reRepoName.ReplaceAllStringFunc(escaped, func(m string) string {
		// 避免将像 1.2.1/1.2.2 或日期当仓库名
		if strings.ContainsAny(m, "0123456789") && !strings.ContainsAny(m, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return m
		}
		return "<code>" + m + "</code>"
	})

	// 编号 #123 -> <b>#123</b>
	escaped = reIssueNum.ReplaceAllString(escaped, `<b>$1</b>`)

	// 版本号 v1.2.3 -> <code>v1.2.3</code>
	escaped = reVersion.ReplaceAllString(escaped, `<code>$1</code>`)

	// 严重度 high/critical -> <b>$1</b>
	escaped = reSeverity.ReplaceAllStringFunc(escaped, func(m string) string {
		upper := strings.ToUpper(m)
		if upper == "HIGH" || upper == "CRITICAL" {
			return "<b>" + upper + "</b>"
		}
		return m
	})

	// 版本迁移 1.2.1→1.2.2 -> <code>1.2.1</code> ➔ <code>1.2.2</code>
	escaped = reVersionTransition.ReplaceAllString(escaped, `<code>$1</code> ➔ <code>$2</code>`)

	// 紧急行动项关键词高亮
	escaped = reUrgentKeywords.ReplaceAllString(escaped, `<b><u>$1</u></b>`)

	return escaped
}

func formatPriorityItem(text string) string {
	prefix := "• "
	if strings.Contains(text, "安全告警") {
		prefix = "• 🚨 "
	} else if strings.Contains(text, "CI") || strings.Contains(text, "失败") {
		prefix = "• ❌ "
	} else if strings.Contains(text, "Bug") || strings.Contains(text, "缺陷") || strings.Contains(text, "问题跟踪") {
		prefix = "• 🐛 "
	}
	return prefix + emphasizePrefix(text)
}

func formatReleaseItem(text string) string {
	return "• 🚀 " + emphasizePrefix(text)
}

func formatRoutineItem(text string) string {
	return "• " + text
}

func emphasizePrefix(text string) string {
	// 将 "安全告警：" / "版本发布：" / "问题跟踪：" 等前缀加粗
	for _, kw := range []string{"安全告警：", "版本发布：", "问题跟踪：", "依赖更新：", "CI 失败：", "缺陷修复："} {
		if strings.HasPrefix(text, kw) {
			return "<b>" + kw + "</b> " + text[len(kw):]
		}
	}
	return text
}

// FormatRelativeTime 计算 target 相对于 now 的自然语言相对时间（如“刚刚”、“10 分钟前”、“2 小时前”）。
func FormatRelativeTime(target, now time.Time) string {
	if target.IsZero() {
		return ""
	}
	diff := now.Sub(target)
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins <= 0 {
			mins = 1
		}
		return strconv.Itoa(mins) + " 分钟前"
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours <= 0 {
			hours = 1
		}
		return strconv.Itoa(hours) + " 小时前"
	default:
		days := int(diff.Hours() / 24)
		if days <= 0 {
			days = 1
		}
		return strconv.Itoa(days) + " 天前"
	}
}
