package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ScheduledJob 保存后台任务队列项。
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
