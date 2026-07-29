-- 列表项本地忽略标记（长期打开的 Issue/PR、无需关注的 Actions/告警）。
ALTER TABLE "work_items" ADD COLUMN "ignored" boolean NOT NULL DEFAULT false;
ALTER TABLE "workflow_runs" ADD COLUMN "ignored" boolean NOT NULL DEFAULT false;
ALTER TABLE "security_alerts" ADD COLUMN "ignored" boolean NOT NULL DEFAULT false;
