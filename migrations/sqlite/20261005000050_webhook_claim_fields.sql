ALTER TABLE `webhook_deliveries` ADD COLUMN `claim_token` text NOT NULL DEFAULT '';
ALTER TABLE `webhook_deliveries` ADD COLUMN `claim_version` integer NOT NULL DEFAULT 0;
ALTER TABLE `webhook_deliveries` ADD COLUMN `claimed_by` text NOT NULL DEFAULT '';
ALTER TABLE `webhook_deliveries` ADD COLUMN `claimed_until` datetime NULL;
ALTER TABLE `webhook_deliveries` ADD COLUMN `attempt_count` integer NOT NULL DEFAULT 0;
ALTER TABLE `webhook_deliveries` ADD COLUMN `last_error_code` text NOT NULL DEFAULT '';
CREATE INDEX `webhook_deliveries_claim_check` ON `webhook_deliveries` (`status`, `claimed_until`);
