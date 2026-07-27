package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditLog 定义只能追加的审计记录。
type AuditLog struct {
	ent.Schema
}

// Fields 返回审计字段。
func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("action"),
		field.String("actor_type"),
		field.String("actor_id"),
		field.String("target_type"),
		field.String("target_id"),
		field.JSON("metadata_json", json.RawMessage{}),
		field.String("ip_address"),
		field.Time("created_at").Immutable(),
	}
}

// Indexes 返回常用审计读取索引。
func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("action"),
	}
}
