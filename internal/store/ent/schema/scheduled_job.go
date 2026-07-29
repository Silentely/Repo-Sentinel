package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ScheduledJob 保存后台作业队列项。
type ScheduledJob struct {
	ent.Schema
}

func (ScheduledJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("job_type"),
		field.String("payload_json").Default("{}"),
		field.String("status").Default("pending"), // pending | running | completed | failed
		field.Int("attempt_count").Default(0),
		field.Time("run_at"),
		field.Time("locked_until").Optional().Nillable(),
		field.String("last_error_code").Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (ScheduledJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "run_at"),
		index.Fields("job_type", "status"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (ScheduledJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "scheduled_jobs"},
	}
}
