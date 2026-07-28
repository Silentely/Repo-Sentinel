package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NotificationOutbox 保存待投递通知。
type NotificationOutbox struct {
	ent.Schema
}

func (NotificationOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("channel_id"),
		field.String("event_id").Optional().Nillable(),
		field.String("aggregate_key").Default(""),
		field.String("idempotency_key"),
		field.String("status").Default("pending"), // pending | sending | sent | dead
		field.Int("attempt_count").Default(0),
		field.Time("next_attempt_at"),
		field.Time("locked_until").Optional().Nillable(),
		field.String("last_error_code").Default(""),
		field.String("title").Default(""),
		field.String("body_text"),
		field.JSON("body_json", map[string]any{}).Optional(),
		field.String("parse_mode").Default("HTML"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (NotificationOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("idempotency_key").Unique(),
		index.Fields("status", "next_attempt_at"),
		index.Fields("channel_id", "status"),
		index.Fields("created_at"),
	}
}
