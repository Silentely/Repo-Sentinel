-- outbox 冗余仓库列：Release 通知按仓库取消未投递记录时只凭 status + 本列走一次
-- 批量 UPDATE，避免全量拉取 pending 行再逐条回查关联事件（事件可能已被仓库级联删除）
ALTER TABLE "notification_outbox" ADD COLUMN "repository_full_name" text NOT NULL DEFAULT '';
CREATE INDEX "notificationoutbox_status_repository_full_name" ON "notification_outbox" ("status", "repository_full_name");

-- 回填：body_json 内直接带 kind 与 repository 的新写入行
UPDATE "notification_outbox"
SET "repository_full_name" = "body_json"->>'repository'
WHERE "body_json"->>'kind' = 'release'
  AND "body_json"->>'repository' IS NOT NULL;
