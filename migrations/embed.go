// Package migrations 嵌入 Store 启动所需的 Atlas 版本目录。
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed sqlite/*.sql sqlite/atlas.sum postgres/*.sql postgres/atlas.sum
var files embed.FS

// Dialect 返回指定方言的只读迁移目录。
func Dialect(name string) (fs.FS, error) {
	if name != "sqlite" && name != "postgres" {
		return nil, fmt.Errorf("unsupported migration dialect")
	}
	return fs.Sub(files, name)
}
