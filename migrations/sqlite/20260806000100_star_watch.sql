-- star/watch 能力开关 + 仓库指标按天快照表
ALTER TABLE `repositories` ADD COLUMN `stars_enabled` bool NOT NULL DEFAULT (1);
ALTER TABLE `repositories` ADD COLUMN `watches_enabled` bool NOT NULL DEFAULT (1);

-- Create "repo_stat_snapshots" table.
CREATE TABLE `repo_stat_snapshots` (
  `id` text NOT NULL,
  `repository_id` text NOT NULL,
  `metric` text NOT NULL,
  `value` integer NOT NULL,
  `sample_date` text NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE UNIQUE INDEX `repostatsnapshot_repository_id_metric_sample_date` ON `repo_stat_snapshots` (`repository_id`, `metric`, `sample_date`);
CREATE INDEX `repostatsnapshot_metric_sample_date` ON `repo_stat_snapshots` (`metric`, `sample_date`);
