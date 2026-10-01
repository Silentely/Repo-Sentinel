-- 机器账号（Bot）识别与渠道降噪过滤
ALTER TABLE "work_items" ADD COLUMN "author_is_bot" boolean NOT NULL DEFAULT false;
ALTER TABLE "events" ADD COLUMN "sender_is_bot" boolean NOT NULL DEFAULT false;
ALTER TABLE "notification_channels" ADD COLUMN "ignore_bots" boolean NOT NULL DEFAULT false;
