CREATE TABLE `system_leases` (
  `id` text PRIMARY KEY NOT NULL,
  `task_name` text NOT NULL,
  `holder_id` text NOT NULL DEFAULT '',
  `acquired_at` datetime NOT NULL,
  `expires_at` datetime NOT NULL,
  `fencing_token` integer NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX `system_leases_task_name` ON `system_leases` (`task_name`);
CREATE INDEX `system_leases_expires_at` ON `system_leases` (`expires_at`);
