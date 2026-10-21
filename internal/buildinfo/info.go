package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	version      = "dev"
	gitSHA       = "unknown"
	gitBranch    = "unknown"
	buildTime    = "unknown"
	buildChannel = "local"
)

// Info 描述当前二进制的构建元数据。
type Info struct {
	Version      string
	GitSHA       string
	GitBranch    string
	BuildTime    string
	BuildChannel string
	GoVersion    string
}

// Current 返回链接阶段注入的构建元数据及安全的开发回退值。
func Current() Info {
	return Info{
		Version:      version,
		GitSHA:       gitSHA,
		GitBranch:    gitBranch,
		BuildTime:    buildTime,
		BuildChannel: buildChannel,
		GoVersion:    runtime.Version(),
	}
}

// UserAgent 返回遵循标准格式的出站 HTTP User-Agent 标识，版本号跟随编译/发布版本动态变化。
// 格式如：Repo-Sentinel/<Version> (<Component>; +https://github.com/Silentely/Repo-Sentinel)
func UserAgent(component ...string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "dev"
	}
	comp := ""
	if len(component) > 0 && strings.TrimSpace(component[0]) != "" {
		comp = strings.TrimSpace(component[0]) + "; "
	}
	return fmt.Sprintf("Repo-Sentinel/%s (%s+https://github.com/Silentely/Repo-Sentinel)", v, comp)
}
