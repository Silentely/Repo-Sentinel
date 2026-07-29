-- Add per-repository capability toggle columns.
ALTER TABLE "repositories" ADD COLUMN "monitor_enabled" boolean NOT NULL DEFAULT TRUE;
ALTER TABLE "repositories" ADD COLUMN "issues_enabled" boolean NOT NULL DEFAULT TRUE;
ALTER TABLE "repositories" ADD COLUMN "pr_enabled" boolean NOT NULL DEFAULT TRUE;
ALTER TABLE "repositories" ADD COLUMN "actions_enabled" boolean NOT NULL DEFAULT TRUE;
ALTER TABLE "repositories" ADD COLUMN "alerts_enabled" boolean NOT NULL DEFAULT TRUE;
