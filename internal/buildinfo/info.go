package buildinfo

import (
	"runtime"
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
