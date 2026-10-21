package ai

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

var (
	// ErrEmptyDiff 表示传入的 PR diff 为空。
	ErrEmptyDiff = errors.New("ai: empty diff")
	// ErrUnrecognizedDiff 表示 diff 格式未知或无法解析为标准 git/unified diff。
	ErrUnrecognizedDiff = errors.New("ai: unrecognized diff format")
)

// LocalReviewGate 根据文件列表的事实快速进行本地门控决策。
// 返回 "deep"（需要深度审查）、"consult"（需咨询 Jev）或 "skip"（可跳过审查）。
func LocalReviewGate(files []jev.FileFact) string {
	if len(files) == 0 {
		return "deep"
	}
	ambiguous := false
	for _, file := range files {
		if file.Path == "" || file.Sensitive || !file.Documentation {
			return "deep"
		}
		ambiguous = ambiguous || file.Ambiguous
	}
	if ambiguous {
		return "consult"
	}
	return "skip"
}

// AnalyzeReviewFiles 解析 PR Diff 并生成符合 Jev 规格的文件事实列表。
// 封装现有 diff 解析与启发式扫描，保留文件名、增删行数、分类与敏感命中。
// parse 失败返回明确 error，避免静默吞掉未知 diff 格式。
func AnalyzeReviewFiles(diff string) ([]jev.FileFact, error) {
	if strings.TrimSpace(diff) == "" {
		return nil, ErrEmptyDiff
	}

	hasGitDiff := strings.Contains(diff, "diff --git ")
	hasUnified := strings.Contains(diff, "--- ") && strings.Contains(diff, "+++ ")
	if !hasGitDiff && !hasUnified {
		return nil, ErrUnrecognizedDiff
	}

	rep := scanDiffHeuristics(diff)
	if len(rep.fileManifest) == 0 {
		return nil, ErrUnrecognizedDiff
	}

	// 嗅探 diff 中的二进制文件标记或删除/重命名标记
	diffLower := strings.ToLower(diff)
	hasBinary := strings.Contains(diffLower, "binary files ") || strings.Contains(diffLower, "git binary patch")

	facts := make([]jev.FileFact, 0, len(rep.fileManifest))
	for _, f := range rep.fileManifest {
		path := strings.TrimSpace(f.path)
		if path == "" {
			facts = append(facts, jev.FileFact{Path: ""})
			continue
		}

		clean := filepath.ToSlash(strings.ToLower(path))
		ext := strings.ToLower(filepath.Ext(clean))

		// 1. 判断是否为普通文档后缀
		isDocExt := ext == ".md" || ext == ".markdown" || ext == ".mdx" || ext == ".rst" || ext == ".txt"

		// 2. 排除脚本目录、配置文件与构建目录
		if isDocExt {
			parts := strings.Split(clean, "/")
			for _, p := range parts {
				switch p {
				case "scripts", "script", "bin", "tools", "tool", ".github", "config", "configs", ".husky":
					isDocExt = false
				}
			}
		}

		// 3. 判断是否为敏感资产或敏感路径
		sensitive := false
		if classifySensitiveAsset(path) != "" {
			sensitive = true
		} else if isSensitivePath(clean) {
			sensitive = true
		} else if f.isProdCode || f.isEdge {
			// 生产代码或边缘网关代码不属于文档
		}

		// 全局二进制标记若出现在当前 diff 中，安全起见标记敏感
		if hasBinary && isBinaryCandidate(clean) {
			sensitive = true
			isDocExt = false
		}

		// 4. 文档判定
		documentation := isDocExt && !sensitive && !f.isTest && !f.isNoise

		// 5. 模糊性 (Ambiguous) 判定：虽然在文档集合内，但存在潜在风险信号
		ambiguous := false
		if documentation {
			// 若为 .txt 纯文本、或疑似钓鱼/引流文档、或有启发式警告，或不在标准文档目录/根文档
			if ext == ".txt" {
				ambiguous = true
			} else if suspiciousDocFileRegex.MatchString(path) {
				ambiguous = true
			} else if len(rep.flaggedSpamDocs) > 0 {
				ambiguous = true
			} else if !isStandardDocLocation(clean) {
				ambiguous = true
			}
		}

		facts = append(facts, jev.FileFact{
			Path:          path,
			Documentation: documentation,
			Sensitive:     sensitive,
			Ambiguous:     ambiguous,
			Additions:     f.adds,
			Deletions:     f.dels,
		})
	}

	return facts, nil
}

func isSensitivePath(clean string) bool {
	if strings.HasPrefix(clean, ".github/") || strings.HasPrefix(clean, ".circleci/") || clean == ".gitlab-ci.yml" {
		return true
	}
	parts := strings.Split(clean, "/")
	for _, p := range parts {
		switch p {
		case "auth", "oauth", "security", "secrets", "secret", "permissions", "permission", "rbac", "keys":
			return true
		case "migrations", "migrate":
			return true
		}
	}
	base := filepath.Base(clean)
	if strings.Contains(base, "auth") || strings.Contains(base, "token") || strings.Contains(base, "secret") ||
		strings.Contains(base, "credential") || strings.Contains(base, "password") || strings.Contains(base, "privilege") {
		return true
	}
	return false
}

func isBinaryCandidate(clean string) bool {
	ext := strings.ToLower(filepath.Ext(clean))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".tar", ".gz", ".exe", ".bin", ".so", ".dylib", ".dll":
		return true
	}
	return false
}

func isStandardDocLocation(clean string) bool {
	if strings.HasPrefix(clean, "docs/") || strings.HasPrefix(clean, "doc/") || strings.HasPrefix(clean, "documentation/") {
		return true
	}
	base := filepath.Base(clean)
	switch base {
	case "readme.md", "readme.rst", "readme.txt", "contributing.md", "contributing.rst",
		"license", "license.md", "license.txt", "changelog.md", "changelog.rst",
		"code_of_conduct.md", "security.md":
		return true
	}
	return false
}
