package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminSession 定义管理员会话持久化结构。
type AdminSession struct {
	ent.Schema
}

// Fields 返回会话字段。
func (AdminSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("admin_id").Immutable(),
		field.String("token_hash").Sensitive(),
		field.String("csrf_hash").Sensitive(),
		field.Time("created_at").Immutable(),
		field.Time("expires_at"),
		field.Time("last_seen_at"),
		field.String("ip_address"),
		field.String("user_agent"),
	}
}

// Edges 返回会话所属管理员关系。
func (AdminSession) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("admin", AdminAccount.Type).
			Ref("sessions").
			Field("admin_id").
			Unique().
			Required().
			Immutable(),
	}
}

// Indexes 返回会话查询与唯一性索引。
func (AdminSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Fields("expires_at"),
	}
}
