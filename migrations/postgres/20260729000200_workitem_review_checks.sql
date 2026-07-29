-- Add PR Review and Check Runs fields to work_items.
ALTER TABLE "work_items" ADD COLUMN "review_state" text NOT NULL DEFAULT '';
ALTER TABLE "work_items" ADD COLUMN "review_decision" text NOT NULL DEFAULT '';
ALTER TABLE "work_items" ADD COLUMN "reviewers" jsonb;
ALTER TABLE "work_items" ADD COLUMN "check_status" text NOT NULL DEFAULT '';
ALTER TABLE "work_items" ADD COLUMN "check_conclusion" text NOT NULL DEFAULT '';
ALTER TABLE "work_items" ADD COLUMN "checks_total" integer NOT NULL DEFAULT 0;
ALTER TABLE "work_items" ADD COLUMN "checks_passed" integer NOT NULL DEFAULT 0;
