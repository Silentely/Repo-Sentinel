package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SyncCursor 保存资源同步游标。
type SyncCursor struct {
	ent.Schema
}

func (SyncCursor) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("repository_id"),
		field.String("resource"), // issues | pulls | workflows | dependabot | code_scanning | secret_scanning
		field.String("cursor_value").Default(""),
		field.String("etag").Default(""),
		field.Time("last_success_at").Optional().Nillable(),
		field.String("last_error_code").Default(""),
		field.Time("updated_at"),
	}
}

func (SyncCursor) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "resource").Unique(),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (SyncCursor) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sync_cursors"},
	}
}

