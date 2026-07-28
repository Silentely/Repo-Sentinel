package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WorkItem 保存 Issue 与 PR（共用编号空间）。
type WorkItem struct {
	ent.Schema
}

func (WorkItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("repository_id"),
		field.Int("number"),
		field.String("kind"), // issue | pull_request
		field.String("state"),
		field.String("title"),
		field.String("author").Default(""),
		field.JSON("labels_json", []any{}).Optional(),
		field.JSON("assignees_json", []any{}).Optional(),
		field.String("milestone").Default(""),
		field.Bool("draft").Default(false),
		field.Bool("merged").Default(false),
		field.String("html_url").Default(""),
		field.Time("source_updated_at"),
		field.String("state_hash"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (WorkItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "number").Unique(),
		index.Fields("repository_id", "kind", "state"),
		index.Fields("updated_at"),
	}
}
