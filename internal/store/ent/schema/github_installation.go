package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GitHubInstallation 保存 GitHub App 安装信息。
type GitHubInstallation struct {
	ent.Schema
}

func (GitHubInstallation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.Int64("installation_id"),
		field.String("account_login"),
		field.String("account_type"),
		field.String("target_type").Default(""),
		field.JSON("permissions_json", map[string]any{}).Optional(),
		field.String("suspended").Default("false"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (GitHubInstallation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("installation_id").Unique(),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (GitHubInstallation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "github_installations"},
	}
}
