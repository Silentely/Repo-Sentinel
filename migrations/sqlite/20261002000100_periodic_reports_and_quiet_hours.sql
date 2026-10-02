-- 周期性报告精细化订阅与免打扰时段
ALTER TABLE `notification_channels` ADD COLUMN `receive_daily_digest` boolean NOT NULL DEFAULT (1);
ALTER TABLE `notification_channels` ADD COLUMN `receive_weekly_report` boolean NOT NULL DEFAULT (1);
ALTER TABLE `notification_channels` ADD COLUMN `receive_monthly_report` boolean NOT NULL DEFAULT (1);
ALTER TABLE `notification_channels` ADD COLUMN `quiet_hours_enabled` boolean NOT NULL DEFAULT (0);
ALTER TABLE `notification_channels` ADD COLUMN `quiet_hours_start` text NOT NULL DEFAULT ('22:00');
ALTER TABLE `notification_channels` ADD COLUMN `quiet_hours_end` text NOT NULL DEFAULT ('08:00');
ALTER TABLE `notification_channels` ADD COLUMN `quiet_hours_tz` text NOT NULL DEFAULT ('UTC');

-- 历史回填：对齐既有 digest_enabled 设置，避免旧用户被静默误开启
UPDATE `notification_channels` SET
  `receive_daily_digest` = CASE WHEN `digest_enabled` THEN 1 ELSE 0 END,
  `receive_weekly_report` = CASE WHEN `digest_enabled` THEN 1 ELSE 0 END,
  `receive_monthly_report` = CASE WHEN `digest_enabled` THEN 1 ELSE 0 END;
