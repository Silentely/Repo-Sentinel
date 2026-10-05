-- Channel routing and outbox suppression audit columns
ALTER TABLE `notification_channels` ADD COLUMN `repo_pattern` text NOT NULL DEFAULT "";
ALTER TABLE `notification_channels` ADD COLUMN `branch_filter` text NOT NULL DEFAULT "";
ALTER TABLE `notification_channels` ADD COLUMN `min_severity` text NOT NULL DEFAULT "low";
ALTER TABLE `notification_outbox` ADD COLUMN `suppressed_reason` text NOT NULL DEFAULT "";
