-- WebhookDelivery 复合索引：覆盖 status/event_type 过滤与 received_at DESC, id DESC 排序。
-- 同时删除被 (status, received_at, id) 最左前缀覆盖的单列 webhookdelivery_status 冗余索引。
DROP INDEX IF EXISTS "webhookdelivery_status";
CREATE INDEX "webhookdelivery_status_received_at_id" ON "webhook_deliveries" ("status", "received_at", "id");
CREATE INDEX "webhookdelivery_event_type_received_at_id" ON "webhook_deliveries" ("event_type", "received_at", "id");
