-- GitHub ID 统一使用 bigint：int4 上限 2147483647，workflow run ID 已达 300 亿级；
-- events.workflow_run_id 直接保存 GitHub run ID，int4 会在插入时编码失败；
-- repositories.github_repo_id 与 github_installations.installation_id 同属 GitHub ID，一并修正避免后续溢出。
ALTER TABLE "events" ALTER COLUMN "workflow_run_id" TYPE bigint;
ALTER TABLE "repositories" ALTER COLUMN "github_repo_id" TYPE bigint;
ALTER TABLE "github_installations" ALTER COLUMN "installation_id" TYPE bigint;
