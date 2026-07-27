-- Create "admin_accounts" table.
CREATE TABLE `admin_accounts` (
  `id` text NOT NULL,
  `username` text NOT NULL,
  `username_normalized` text NOT NULL,
  `password_hash` text NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `password_changed_at` datetime NOT NULL,
  `singleton_slot` integer NOT NULL DEFAULT 1,
  PRIMARY KEY (`id`)
);
CREATE UNIQUE INDEX `adminaccount_username_normalized` ON `admin_accounts` (`username_normalized`);
CREATE UNIQUE INDEX `adminaccount_singleton_slot` ON `admin_accounts` (`singleton_slot`);

-- Create "admin_sessions" table.
CREATE TABLE `admin_sessions` (
  `id` text NOT NULL,
  `token_hash` text NOT NULL,
  `csrf_hash` text NOT NULL,
  `created_at` datetime NOT NULL,
  `expires_at` datetime NOT NULL,
  `last_seen_at` datetime NOT NULL,
  `ip_address` text NOT NULL,
  `user_agent` text NOT NULL,
  `admin_id` text NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `admin_sessions_admin_accounts_sessions`
    FOREIGN KEY (`admin_id`) REFERENCES `admin_accounts` (`id`) ON DELETE CASCADE
);
CREATE UNIQUE INDEX `adminsession_token_hash` ON `admin_sessions` (`token_hash`);
CREATE INDEX `adminsession_expires_at` ON `admin_sessions` (`expires_at`);

-- Create "audit_logs" table.
CREATE TABLE `audit_logs` (
  `id` text NOT NULL,
  `action` text NOT NULL,
  `actor_type` text NOT NULL,
  `actor_id` text NOT NULL,
  `target_type` text NOT NULL,
  `target_id` text NOT NULL,
  `metadata_json` json NOT NULL,
  `ip_address` text NOT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `auditlog_created_at` ON `audit_logs` (`created_at`);
CREATE INDEX `auditlog_action` ON `audit_logs` (`action`);

-- Create "system_settings" table.
CREATE TABLE `system_settings` (
  `id` text NOT NULL,
  `key` text NOT NULL,
  `value_json` json NOT NULL,
  `updated_at` datetime NOT NULL,
  `updated_by` text NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE UNIQUE INDEX `systemsetting_key` ON `system_settings` (`key`);
