package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SystemSetting 定义唯一键 JSON 系统设置。
type SystemSetting struct {
	ent.Schema
}

// Fields 返回系统设置字段。
func (SystemSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("key").Immutable(),
		field.JSON("value_json", json.RawMessage{}),
		field.Time("updated_at"),
		field.String("updated_by"),
	}
}

// Indexes 在数据库层保证设置键唯一。
func (SystemSetting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key").Unique(),
	}
}
