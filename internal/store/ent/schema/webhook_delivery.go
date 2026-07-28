package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WebhookDelivery 保存 GitHub delivery 去重与处理状态。
type WebhookDelivery struct {
	ent.Schema
}

func (WebhookDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("delivery_id"),
		field.String("event_type"),
		field.String("action").Default(""),
		field.String("repository_full_name").Default(""),
		field.String("status").Default("accepted"), // accepted | processed | failed | duplicate
		field.String("error_code").Default(""),
		field.Bytes("payload").Optional(),
		field.Time("received_at").Immutable(),
		field.Time("processed_at").Optional().Nillable(),
	}
}

func (WebhookDelivery) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("delivery_id").Unique(),
		index.Fields("status"),
		index.Fields("received_at"),
	}
}
