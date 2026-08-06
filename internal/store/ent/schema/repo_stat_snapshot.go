package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RepoStatSnapshot 仓库指标按天快照（当前仅 stargazers，metric 预留扩展）。
type RepoStatSnapshot struct {
	ent.Schema
}

func (RepoStatSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("repository_id"),
		field.String("metric"),
		field.Int64("value"),
		field.String("sample_date"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (RepoStatSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "metric", "sample_date").Unique(),
		index.Fields("metric", "sample_date"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (RepoStatSnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "repo_stat_snapshots"},
	}
}
