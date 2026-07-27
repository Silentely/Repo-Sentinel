package buildinfo

import (
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

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

// ReadVersionFile 读取、去除首尾空白并校验 X.Y.Z 格式的 SemVer。
func ReadVersionFile(r io.Reader) (string, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("读取版本文件: %w", err)
	}

	value := strings.TrimSpace(string(content))
	if !versionPattern.MatchString(value) {
		return "", fmt.Errorf("版本 %q 不是有效的 X.Y.Z SemVer", value)
	}

	return value, nil
}
