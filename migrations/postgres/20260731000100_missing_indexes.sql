-- 补齐 schema 已声明但建列迁移遗漏的过滤索引，并覆盖同步调度排序。
CREATE INDEX "workitem_ignored_kind_state" ON "work_items" ("ignored", "kind", "state");
CREATE INDEX "workitem_ignored_review_decision" ON "work_items" ("ignored", "review_decision");
CREATE INDEX "workitem_ignored_check_status" ON "work_items" ("ignored", "check_status");
CREATE INDEX "workflowrun_ignored" ON "workflow_runs" ("ignored");
CREATE INDEX "securityalert_ignored" ON "security_alerts" ("ignored");
CREATE INDEX "repository_type_last_synced_at" ON "repositories" ("type", "last_synced_at");
