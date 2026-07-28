package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NotificationChannel 保存通知渠道配置。
type NotificationChannel struct {
	ent.Schema
}

func (NotificationChannel) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("channel_type"), // telegram | http_webhook
		field.String("name").Default(""),
		field.Bool("enabled").Default(false),
		field.String("target").Default(""), // chat_id 或 URL
		field.String("secret_envelope").Default(""), // 加密 token/签名密钥
		field.Bool("allow_private").Default(false),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (NotificationChannel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_type", "enabled"),
	}
}
