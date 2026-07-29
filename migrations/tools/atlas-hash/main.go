// Package main 提供 Atlas 迁移校验和自动重算工具。
// 用法: go run . <sqlite|postgres|all>
// 通常通过 `go generate ./migrations/...` 触发。
package main

import (
	"fmt"
	"os"

	"ariga.io/atlas/sql/migrate"
)

func main() {
	dialects := []string{"sqlite", "postgres"}
	if len(os.Args) > 1 && os.Args[1] != "all" {
		dialects = []string{os.Args[1]}
	}
	for _, d := range dialects {
		dir, err := migrate.NewLocalDir(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "打开 %s 目录失败: %v\n", d, err)
			os.Exit(1)
		}
		hf, err := dir.Checksum()
		if err != nil {
			fmt.Fprintf(os.Stderr, "计算 %s 校验和失败: %v\n", d, err)
			os.Exit(1)
		}
		if err := migrate.WriteSumFile(dir, hf); err != nil {
			fmt.Fprintf(os.Stderr, "写入 %s 校验和失败: %v\n", d, err)
			os.Exit(1)
		}
		fmt.Printf("✅ %s/atlas.sum 已更新\n", d)
	}
}
