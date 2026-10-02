package main

import (
	"os"
	// 内嵌完整时区数据库：distroless 运行镜像不含 /usr/share/zoneinfo，
	// 若不内嵌则免打扰时区（如 Asia/Shanghai）会静默回退到 UTC，静默时段错位。
	_ "time/tzdata"

	"github.com/Silentely/Repo-Sentinel/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
