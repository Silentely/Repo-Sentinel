package buildinfo

import (
	"strings"
	"testing"
)

func Test未注入构建信息不会伪装正式版本(t *testing.T) {
	info := Current()
	if info.BuildChannel == "release" {
		t.Fatal("本地默认值不得是 release")
	}
	if info.GitSHA == "" || info.BuildTime == "" {
		t.Fatal("开发回退字段必须明确")
	}
}

func TestUserAgent(t *testing.T) {
	ua1 := UserAgent()
	if !strings.HasPrefix(ua1, "Repo-Sentinel/") || !strings.Contains(ua1, "+https://github.com/Silentely/Repo-Sentinel") {
		t.Fatalf("unexpected UserAgent: %s", ua1)
	}

	ua2 := UserAgent("AIClient")
	if !strings.HasPrefix(ua2, "Repo-Sentinel/") || !strings.Contains(ua2, "(AIClient; +https://github.com/Silentely/Repo-Sentinel)") {
		t.Fatalf("unexpected UserAgent with component: %s", ua2)
	}
}
