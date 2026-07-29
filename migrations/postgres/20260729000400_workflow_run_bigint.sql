-- GitHub Actions run_id / workflow_id 已超过 int4 最大值（2,147,483,647），需要升级为 bigint。
ALTER TABLE "workflow_runs" ALTER COLUMN "github_run_id" TYPE bigint;
ALTER TABLE "workflow_runs" ALTER COLUMN "github_workflow_id" TYPE bigint;
