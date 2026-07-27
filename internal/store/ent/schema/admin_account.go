package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminAccount 定义唯一管理员账号的持久化结构。
type AdminAccount struct {
	ent.Schema
}

// Fields 返回管理员账号字段。
func (AdminAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("username"),
		field.String("username_normalized"),
		field.String("password_hash").Sensitive(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
		field.Time("password_changed_at"),
		field.Int("singleton_slot").Default(1).Immutable(),
	}
}

// Edges 返回管理员拥有的会话关系。
func (AdminAccount) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sessions", AdminSession.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes 在数据库层同时保证用户名唯一和单管理员约束。
func (AdminAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("username_normalized").Unique(),
		index.Fields("singleton_slot").Unique(),
	}
}
