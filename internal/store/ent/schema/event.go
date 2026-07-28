package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Event 保存规范化业务事件。
type Event struct {
	ent.Schema
}

func (Event) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("source"), // webhook | reconcile | external_poll | system
		field.String("kind"),
		field.String("action"),
		field.String("repository_id").Optional().Nillable(),
		field.Int("subject_number").Optional().Nillable(),
		field.String("title").Default(""),
		field.String("severity").Default(""),
		field.String("actor").Default(""),
		field.Int64("workflow_run_id").Optional().Nillable(),
		field.String("workflow_conclusion").Default(""),
		field.Time("occurred_at"),
		field.Time("source_updated_at").Optional().Nillable(),
		field.String("html_url").Default(""),
		field.JSON("payload_summary", map[string]any{}).Optional(),
		field.Bool("suppress_notification").Default(false),
		field.String("dedupe_fingerprint"),
		field.String("state_hash").Default(""),
		field.Time("created_at").Immutable(),
	}
}

func (Event) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("dedupe_fingerprint").Unique(),
		index.Fields("repository_id", "kind", "occurred_at"),
		index.Fields("occurred_at"),
		index.Fields("created_at"),
	}
}
