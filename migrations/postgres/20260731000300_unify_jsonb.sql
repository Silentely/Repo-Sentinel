-- 统一 JSON 列类型为 jsonb（ent field.TypeJSON 的 PostgreSQL 映射），消除 json/jsonb 混用。
ALTER TABLE "github_installations" ALTER COLUMN "permissions_json" TYPE jsonb USING "permissions_json"::jsonb;
ALTER TABLE "work_items" ALTER COLUMN "labels_json" TYPE jsonb USING "labels_json"::jsonb;
ALTER TABLE "work_items" ALTER COLUMN "assignees_json" TYPE jsonb USING "assignees_json"::jsonb;
ALTER TABLE "events" ALTER COLUMN "payload_summary" TYPE jsonb USING "payload_summary"::jsonb;
ALTER TABLE "notification_outbox" ALTER COLUMN "body_json" TYPE jsonb USING "body_json"::jsonb;
ALTER TABLE "notification_channels" ALTER COLUMN "event_kinds" TYPE jsonb USING "event_kinds"::jsonb;
