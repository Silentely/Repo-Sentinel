-- 渠道订阅控制：event_kinds NULL 表示订阅全部实时类型；digest_enabled 默认开启。
ALTER TABLE "notification_channels" ADD COLUMN "event_kinds" json NULL;
ALTER TABLE "notification_channels" ADD COLUMN "digest_enabled" boolean NOT NULL DEFAULT true;
