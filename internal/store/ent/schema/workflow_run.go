package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WorkflowRun 保存 Actions 运行状态。
type WorkflowRun struct {
	ent.Schema
}

func (WorkflowRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("repository_id"),
		field.Int64("github_run_id"),
		field.Int64("github_workflow_id"),
		field.String("workflow_name").Default(""),
		field.Int("run_number").Default(0),
		field.String("event").Default(""),
		field.String("head_branch").Default(""),
		field.String("head_sha").Default(""),
		field.String("status").Default(""),
		field.String("conclusion").Optional().Nillable(),
		field.String("previous_conclusion").Optional().Nillable(),
		field.String("actor").Default(""),
		field.Int("run_attempt").Default(1),
		field.String("html_url").Default(""),
		field.Time("run_started_at").Optional().Nillable(),
		field.Time("run_updated_at"),
		field.Time("run_completed_at").Optional().Nillable(),
		field.String("state_hash"),
		// ignored：用户手动忽略的运行记录，列表默认隐藏。
		field.Bool("ignored").Default(false),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (WorkflowRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "github_run_id").Unique(),
		index.Fields("repository_id", "github_workflow_id", "head_branch"),
		index.Fields("conclusion"),
		index.Fields("ignored"),
		index.Fields("run_updated_at"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (WorkflowRun) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "workflow_runs"},
	}
}
