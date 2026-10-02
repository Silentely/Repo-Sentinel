-- 周期性报告精细化订阅与免打扰时段
ALTER TABLE "notification_channels" ADD COLUMN "receive_daily_digest" boolean NOT NULL DEFAULT true;
ALTER TABLE "notification_channels" ADD COLUMN "receive_weekly_report" boolean NOT NULL DEFAULT true;
ALTER TABLE "notification_channels" ADD COLUMN "receive_monthly_report" boolean NOT NULL DEFAULT true;
ALTER TABLE "notification_channels" ADD COLUMN "quiet_hours_enabled" boolean NOT NULL DEFAULT false;
ALTER TABLE "notification_channels" ADD COLUMN "quiet_hours_start" character varying NOT NULL DEFAULT '22:00';
ALTER TABLE "notification_channels" ADD COLUMN "quiet_hours_end" character varying NOT NULL DEFAULT '08:00';
ALTER TABLE "notification_channels" ADD COLUMN "quiet_hours_tz" character varying NOT NULL DEFAULT 'UTC';

-- 历史回填：对齐既有 digest_enabled 设置，避免旧用户被静默误开启
UPDATE "notification_channels" SET
  "receive_daily_digest" = "digest_enabled",
  "receive_weekly_report" = "digest_enabled",
  "receive_monthly_report" = "digest_enabled";
