package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NotificationChannel 保存通知渠道配置。
type NotificationChannel struct {
	ent.Schema
}

func (NotificationChannel) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("channel_type"), // telegram | http_webhook | slack
		field.String("name").Default(""),
		field.Bool("enabled").Default(false),
		field.String("target").Default(""),          // chat_id 或 URL
		field.String("secret_envelope").Default(""), // 加密 token/签名密钥
		field.Bool("allow_private").Default(false),
		field.JSON("event_kinds", []string{}).Optional(),   // 订阅的实时通知类型；NULL=全部
		field.Bool("digest_enabled").Default(true),         // 是否接收每日汇总（兼容旧版字段）
		field.Bool("receive_daily_digest").Default(true),   // 是否接收每日简报
		field.Bool("receive_weekly_report").Default(true),  // 是否接收每周报告
		field.Bool("receive_monthly_report").Default(true), // 是否接收每月总结
		field.Bool("quiet_hours_enabled").Default(false),   // 免打扰时段开关
		field.String("quiet_hours_start").Default("22:00"), // 免打扰开始时间（HH:MM）
		field.String("quiet_hours_end").Default("08:00"),   // 免打扰结束时间（HH:MM）
		field.String("quiet_hours_tz").Default("UTC"),      // 免打扰时区（如 Asia/Shanghai）
		field.Bool("ignore_bots").Default(false),           // 免打扰：忽略机器人常规 Issue/PR 动态
		field.String("repo_pattern").Default(""),
		field.String("branch_filter").Default(""),
		field.String("min_severity").Default("low"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (NotificationChannel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_type", "enabled"),
	}
}

// Annotations 固定物理表名，与 Atlas 迁移保持一致。
func (NotificationChannel) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_channels"},
	}
}
