package syncx

import (
	"testing"
)

func TestDependencyRadar_Parsers(t *testing.T) {
	// 1. GoMod 解析
	goModSample := `
module github.com/my-org/my-project

go 1.24

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/oklog/ulid/v2 v2.1.0 // indirect
)

require github.com/stretchr/testify v1.8.4
`
	goDeps := ParseGoMod(goModSample)
	if len(goDeps) < 2 {
		t.Fatalf("expected at least 2 deps from go.mod, got %d", len(goDeps))
	}
	foundGin := false
	for _, d := range goDeps {
		if d.Name == "github.com/gin-gonic/gin" && d.Version == "v1.9.1" {
			foundGin = true
			break
		}
	}
	if !foundGin {
		t.Errorf("failed to find gin dependency in parsed go.mod: %+v", goDeps)
	}

	// 2. PackageJSON 解析
	pkgJSONSample := `{
		"name": "my-web-app",
		"dependencies": {
			"react": "^19.0.0",
			"axios": "^1.6.0"
		},
		"devDependencies": {
			"typescript": "^5.0.0"
		}
	}`
	npmDeps := ParsePackageJSON(pkgJSONSample)
	if len(npmDeps) != 3 {
		t.Fatalf("expected 3 deps from package.json, got %d", len(npmDeps))
	}
}

func TestDependencyRadar_EvaluateReleaseImpact(t *testing.T) {
	monitoredDeps := map[string][]Dependency{
		"my-org/backend-service": {
			{Name: "github.com/gin-gonic/gin", Version: "v1.9.0", Ecosystem: "go"},
		},
		"my-org/frontend-app": {
			{Name: "react", Version: "18.2.0", Ecosystem: "npm"},
		},
	}

	// 场景 1: 依赖的库发布破坏性变更 -> high severity + 命中 impacted repo
	ginReleaseNotes := `
## v2.0.0

### Breaking Changes
- Remove Engine.RunTLS in favor of explicit http.Server
- Change default context deadline behavior

### Bug Fixes
- Fix memory leak in router
`
	impact1 := EvaluateReleaseImpact("gin-gonic/gin", ginReleaseNotes, monitoredDeps)
	if !impact1.HasBreakingChanges {
		t.Errorf("expected HasBreakingChanges=true")
	}
	if len(impact1.ImpactedRepoNames) != 1 || impact1.ImpactedRepoNames[0] != "my-org/backend-service" {
		t.Errorf("expected impacted my-org/backend-service, got %+v", impact1.ImpactedRepoNames)
	}
	if impact1.SuggestedSeverity != "high" {
		t.Errorf("expected high severity for breaking change with direct dependency, got %s", impact1.SuggestedSeverity)
	}

	// 场景 2: 未依赖的库发布安全修复 -> medium severity + 无 impacted repo
	unrelatedReleaseNotes := `
## v1.5.0
Fixed CVE-2026-12345 in buffer parser
`
	impact2 := EvaluateReleaseImpact("some-org/unrelated-lib", unrelatedReleaseNotes, monitoredDeps)
	if len(impact2.ImpactedRepoNames) != 0 {
		t.Errorf("expected no impacted repos, got %+v", impact2.ImpactedRepoNames)
	}
	if len(impact2.SecurityFixes) == 0 {
		t.Errorf("expected CVE-2026-12345 to be detected")
	}
	if impact2.SuggestedSeverity != "medium" {
		t.Errorf("expected medium severity for unimpacted CVE release, got %s", impact2.SuggestedSeverity)
	}
}
