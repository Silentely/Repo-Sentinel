-- events.subject_number 保存 GitHub 全局递增 ID（star 追踪 release 事件存 release ID）。
-- PG int4 上限 2147483647，GitHub release ID 已进入十亿量级爬升区间，int4 会在写入时编码失败；
-- 与 workflow_run_id 同属 GitHub ID，遵循项目 bigint 约定。SQLite INTEGER 为 64 位，无 DDL 变更。
ALTER TABLE "events" ALTER COLUMN "subject_number" TYPE bigint;
