package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SystemLease represents a distributed lease for cron / scheduler jobs.
type SystemLease struct {
	ent.Schema
}

func (SystemLease) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("task_name").NotEmpty(),
		field.String("holder_id").Default(""),
		field.Time("acquired_at"),
		field.Time("expires_at"),
		field.Int64("fencing_token").Default(0),
	}
}

func (SystemLease) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_name").Unique().StorageKey("system_leases_task_name"),
		index.Fields("expires_at").StorageKey("system_leases_expires_at"),
	}
}

func (SystemLease) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "system_leases"},
	}
}
