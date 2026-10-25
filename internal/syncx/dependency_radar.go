package syncx

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Dependency 依赖项元数据。
type Dependency struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Ecosystem string `json:"ecosystem"` // go, npm, cargo, pypi
}

// ReleaseImpact 发布的潜在影响分析。
type ReleaseImpact struct {
	HasBreakingChanges bool     `json:"has_breaking_changes"`
	BreakingItems      []string `json:"breaking_items,omitempty"`
	SecurityFixes      []string `json:"security_fixes,omitempty"`
	ImpactedRepoNames  []string `json:"impacted_repo_names,omitempty"`
	UpgradeGuidance    string   `json:"upgrade_guidance,omitempty"`
	SuggestedSeverity  string   `json:"suggested_severity"`
}

// ParseGoMod 解析 go.mod 内容中的直接依赖。
func ParseGoMod(content string) []Dependency {
	var deps []Dependency
	lines := strings.Split(content, "\n")
	inRequireBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			continue
		}
		if trimmed == "require (" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && trimmed == ")" {
			inRequireBlock = false
			continue
		}

		if inRequireBlock || strings.HasPrefix(trimmed, "require ") {
			fields := strings.Fields(trimmed)
			if strings.HasPrefix(trimmed, "require ") {
				fields = fields[1:]
			}
			if len(fields) >= 2 {
				name := fields[0]
				ver := fields[1]
				deps = append(deps, Dependency{
					Name:      name,
					Version:   ver,
					Ecosystem: "go",
				})
			}
		}
	}
	return deps
}

// ParsePackageJSON 解析 package.json 中的 dependencies 与 devDependencies。
func ParsePackageJSON(content string) []Dependency {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil
	}

	deps := make([]Dependency, 0, len(manifest.Dependencies)+len(manifest.DevDependencies))
	for name, ver := range manifest.Dependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Ecosystem: "npm"})
	}
	for name, ver := range manifest.DevDependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Ecosystem: "npm"})
	}
	return deps
}

var (
	breakingHeadingRe = regexp.MustCompile(`(?i)(?:^|\n)#{1,4}\s*.*(?:breaking|破坏性|break).*`)
	cveRe             = regexp.MustCompile(`(?i)(?:CVE-\d{4}-\d+|GHSA-[a-z0-9-]+)`)
)

// EvaluateReleaseImpact 分析 Release Notes 中的破坏性变更与安全修复，并比对本地项目依赖。
func EvaluateReleaseImpact(
	releaseRepo string,
	notes string,
	monitoredRepoDeps map[string][]Dependency, // repoName -> dependencies
) ReleaseImpact {
	var breakingItems []string
	var securityFixes []string

	lines := strings.Split(notes, "\n")
	isBreakingSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)

		// 检查标题是否进入破坏性变更段
		if strings.HasPrefix(trimmed, "#") {
			isBreakingSection = strings.Contains(lower, "breaking") ||
				strings.Contains(lower, "破坏性") ||
				strings.Contains(lower, "migration")
			continue
		}

		if isBreakingSection && (strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*")) {
			breakingItems = append(breakingItems, strings.TrimPrefix(strings.TrimPrefix(trimmed, "-"), "*"))
		} else if strings.Contains(lower, "breaking change:") || strings.Contains(lower, "break:") {
			breakingItems = append(breakingItems, trimmed)
		}

		// 检查 CVE / 安全披露
		if matches := cveRe.FindAllString(trimmed, -1); len(matches) > 0 {
			securityFixes = append(securityFixes, matches...)
		}
	}

	hasBreaking := len(breakingItems) > 0
	hasSec := len(securityFixes) > 0

	// 匹配是否有本地监控项目依赖了此发布仓库
	var impacted []string
	relRepoLower := strings.ToLower(releaseRepo)
	baseName := relRepoLower
	if parts := strings.Split(relRepoLower, "/"); len(parts) == 2 {
		baseName = parts[1]
	}

	for repoName, deps := range monitoredRepoDeps {
		for _, d := range deps {
			dLower := strings.ToLower(d.Name)
			if dLower == relRepoLower || strings.HasSuffix(dLower, "/"+baseName) || dLower == baseName {
				impacted = append(impacted, repoName)
				break
			}
		}
	}

	severity := "low"
	var guidance string
	if len(impacted) > 0 {
		if hasSec {
			severity = "high"
			guidance = "发现安全修复，当前项目存在直接依赖，建议尽快评估升级并验证漏洞补丁！"
		} else if hasBreaking {
			severity = "high"
			guidance = "发现破坏性变更 (Breaking Changes)，当前项目存在直接依赖，升级前请核对调用点兼容性！"
		} else {
			severity = "medium"
			guidance = "常规版本发布，当前项目已引入该依赖，可在后续周期安排升级。"
		}
	} else {
		if hasBreaking || hasSec {
			severity = "medium"
			guidance = "发现破坏性变更或安全补丁，当前监控项目未直接依赖，保留技术情报参考。"
		}
	}

	return ReleaseImpact{
		HasBreakingChanges: hasBreaking,
		BreakingItems:      breakingItems,
		SecurityFixes:      securityFixes,
		ImpactedRepoNames:  impacted,
		UpgradeGuidance:    guidance,
		SuggestedSeverity:  severity,
	}
}
