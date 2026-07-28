package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Repository 保存自有或外部公开仓库。
type Repository struct {
	ent.Schema
}

func (Repository) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("type"), // github_installation | external_public
		field.String("sync_status").Default("baseline_sync"),
		field.Int64("github_repo_id").Optional().Nillable(),
		field.String("owner"),
		field.String("name"),
		field.String("full_name"),
		field.String("installation_id").Optional().Nillable(),
		field.Bool("is_archived").Default(false),
		field.Bool("is_private").Default(false),
		field.String("html_url").Default(""),
		field.String("default_branch").Default(""),
		field.Time("baseline_started_at").Optional().Nillable(),
		field.Time("baseline_finished_at").Optional().Nillable(),
		field.Time("last_synced_at").Optional().Nillable(),
		field.String("last_sync_error_code").Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Repository) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("full_name").Unique(),
		index.Fields("github_repo_id"),
		index.Fields("type"),
		index.Fields("sync_status"),
		index.Fields("installation_id"),
	}
}
