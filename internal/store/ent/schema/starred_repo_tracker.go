package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// StarredRepoTracker 用户 star 仓库的 release 追踪记录。
// state 状态机：tracking（轮询中）/ inactive（无 release，7 天复查）/
// disabled（用户停用或用户 unstar）/ unavailable（404/410 删仓或转私有）。
type StarredRepoTracker struct {
	ent.Schema
}

func (StarredRepoTracker) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("full_name"),
		field.String("state"),
		// etag 为 release 条件请求缓存（304 不计限流的依据）。
		field.String("etag").Optional(),
		// last_release_id 为已确认的最新 release id；0 表示未拉取过（作基线，不通知）。
		field.Int64("last_release_id").Default(0),
		field.String("last_release_tag").Optional(),
		field.Time("last_release_published_at").Optional().Nillable(),
		field.Time("no_release_since").Optional().Nillable(),
		field.Time("no_release_recheck_at").Optional().Nillable(),
		field.Time("first_seen_at").Immutable(),
		field.Time("last_poll_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (StarredRepoTracker) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("full_name").Unique(),
		index.Fields("state", "last_poll_at"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (StarredRepoTracker) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "starred_repo_trackers"},
	}
}
