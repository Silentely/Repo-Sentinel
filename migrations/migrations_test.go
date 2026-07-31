package migrations_test

import (
	"fmt"
	"strings"
	"testing"

	"ariga.io/atlas/sql/migrate"

	entmigrate "github.com/Silentely/Repo-Sentinel/internal/store/ent/migrate"
)

func TestAtlasMigrationDirectories(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dir, err := migrate.NewLocalDir(dialect)
			if err != nil {
				t.Fatalf("打开 %s Atlas 目录失败: %v", dialect, err)
			}
			if err := migrate.Validate(dir); err != nil {
				t.Fatalf("校验 %s Atlas 目录失败: %v", dialect, err)
			}
		})
	}
}

// schema 声明的每个索引都必须在迁移目录中有对应 DDL——
// 本仓库曾出现 schema 加索引但迁移只加列的漂移（列表/Dashboard 谓词无索引可走）。
func TestSchemaIndexesHaveMigrations(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dir, err := migrate.NewLocalDir(dialect)
			if err != nil {
				t.Fatalf("打开 %s Atlas 目录失败: %v", dialect, err)
			}
			files, err := dir.Files()
			if err != nil {
				t.Fatalf("读取 %s 迁移文件失败: %v", dialect, err)
			}
			var all strings.Builder
			for _, file := range files {
				all.Write(file.Bytes())
			}
			content := all.String()

			var missing []string
			for _, table := range entmigrate.Tables {
				for _, idx := range table.Indexes {
					if idx.Name == "" {
						continue
					}
					if !strings.Contains(content, idx.Name) {
						missing = append(missing, fmt.Sprintf("%s.%s", table.Name, idx.Name))
					}
				}
			}
			if len(missing) > 0 {
				t.Fatalf("%s 迁移缺少 schema 索引（请新增迁移并重算 atlas.sum）: %s", dialect, strings.Join(missing, ", "))
			}
		})
	}
}
