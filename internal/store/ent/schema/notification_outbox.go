package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
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
		field.String("status").Default("pending"), // pending | sending | sent | dead | cancelled
		field.Int("attempt_count").Default(0),
		field.Time("next_attempt_at"),
		field.Time("locked_until").Optional().Nillable(),
		field.String("last_error_code").Default(""),
		field.String("title").Default(""),
		field.String("body_text"),
		field.JSON("body_json", map[string]any{}).Optional(),
		// 冗余列：Release 通知按仓库取消未投递记录的依据。写入时从 body_json 派生，
		// 使 unstar 取消只凭 status + 本列走一次批量 UPDATE，无需回查关联事件
		// （事件可能已被仓库级联删除）。非 Release 类通知留空，保持取消语义不变。
		field.String("repository_full_name").Default(""),
		field.String("parse_mode").Default("HTML"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (NotificationOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("idempotency_key").Unique(),
		index.Fields("status", "next_attempt_at"),
		index.Fields("status", "repository_full_name"),
		index.Fields("channel_id", "status"),
		index.Fields("created_at"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (NotificationOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_outbox"},
	}
}
