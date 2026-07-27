package migrations_test

import (
	"testing"

	"ariga.io/atlas/sql/migrate"
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
