package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SecurityAlert 保存三类安全告警。
type SecurityAlert struct {
	ent.Schema
}

func (SecurityAlert) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("repository_id"),
		field.String("alert_kind"), // dependabot | code_scanning | secret_scanning
		field.Int("alert_number"),
		field.String("state"),
		field.String("severity").Default(""),
		field.String("rule_or_dependency").Default(""),
		field.String("dismissed_reason").Default(""),
		field.String("html_url").Default(""),
		field.Time("source_updated_at"),
		field.String("state_hash"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (SecurityAlert) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "alert_kind", "alert_number").Unique(),
		index.Fields("state"),
		index.Fields("severity"),
		index.Fields("source_updated_at"),
	}
}
