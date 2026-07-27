// Package web 提供 RepoSentinel 管理控制台的只读嵌入资源。
package web

import (
	"fmt"
	"io/fs"
)

// Files 返回当前构建模式的前端资源，并确保入口页真实存在。
func Files() (fs.FS, error) {
	frontend, err := fs.Sub(embeddedFiles, embeddedRoot)
	if err != nil {
		return nil, fmt.Errorf("open embedded frontend: %w", err)
	}
	entry, err := fs.Stat(frontend, "index.html")
	if err != nil {
		return nil, fmt.Errorf("open embedded frontend index: %w", err)
	}
	if entry.IsDir() {
		return nil, fmt.Errorf("open embedded frontend index: index.html is a directory")
	}
	return frontend, nil
}
