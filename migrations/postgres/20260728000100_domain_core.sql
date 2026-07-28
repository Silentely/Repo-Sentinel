-- Create "github_installations" table.
CREATE TABLE "github_installations" (
  "id" text NOT NULL,
  "installation_id" integer NOT NULL,
  "account_login" text NOT NULL,
  "account_type" text NOT NULL,
  "target_type" text NOT NULL DEFAULT '',
  "permissions_json" json NULL,
  "suspended" text NOT NULL DEFAULT 'false',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "githubinstallation_installation_id" ON "github_installations" ("installation_id");

-- Create "repositories" table.
CREATE TABLE "repositories" (
  "id" text NOT NULL,
  "type" text NOT NULL,
  "sync_status" text NOT NULL DEFAULT 'baseline_sync',
  "github_repo_id" integer NULL,
  "owner" text NOT NULL,
  "name" text NOT NULL,
  "full_name" text NOT NULL,
  "installation_id" text NULL,
  "is_archived" boolean NOT NULL DEFAULT FALSE,
  "is_private" boolean NOT NULL DEFAULT FALSE,
  "html_url" text NOT NULL DEFAULT '',
  "default_branch" text NOT NULL DEFAULT '',
  "baseline_started_at" datetime NULL,
  "baseline_finished_at" datetime NULL,
  "last_synced_at" datetime NULL,
  "last_sync_error_code" text NOT NULL DEFAULT '',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "repository_full_name" ON "repositories" ("full_name");
CREATE INDEX "repository_github_repo_id" ON "repositories" ("github_repo_id");
CREATE INDEX "repository_type" ON "repositories" ("type");
CREATE INDEX "repository_sync_status" ON "repositories" ("sync_status");
CREATE INDEX "repository_installation_id" ON "repositories" ("installation_id");

-- Create "webhook_deliveries" table.
CREATE TABLE "webhook_deliveries" (
  "id" text NOT NULL,
  "delivery_id" text NOT NULL,
  "event_type" text NOT NULL,
  "action" text NOT NULL DEFAULT '',
  "repository_full_name" text NOT NULL DEFAULT '',
  "status" text NOT NULL DEFAULT 'accepted',
  "error_code" text NOT NULL DEFAULT '',
  "payload" bytea NULL,
  "received_at" datetime NOT NULL,
  "processed_at" datetime NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "webhookdelivery_delivery_id" ON "webhook_deliveries" ("delivery_id");
CREATE INDEX "webhookdelivery_status" ON "webhook_deliveries" ("status");
CREATE INDEX "webhookdelivery_received_at" ON "webhook_deliveries" ("received_at");

-- Create "work_items" table.
CREATE TABLE "work_items" (
  "id" text NOT NULL,
  "repository_id" text NOT NULL,
  "number" integer NOT NULL,
  "kind" text NOT NULL,
  "state" text NOT NULL,
  "title" text NOT NULL,
  "author" text NOT NULL DEFAULT '',
  "labels_json" json NULL,
  "assignees_json" json NULL,
  "milestone" text NOT NULL DEFAULT '',
  "draft" boolean NOT NULL DEFAULT FALSE,
  "merged" boolean NOT NULL DEFAULT FALSE,
  "html_url" text NOT NULL DEFAULT '',
  "source_updated_at" datetime NOT NULL,
  "state_hash" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "workitem_repository_id_number" ON "work_items" ("repository_id", "number");
CREATE INDEX "workitem_repository_id_kind_state" ON "work_items" ("repository_id", "kind", "state");
CREATE INDEX "workitem_updated_at" ON "work_items" ("updated_at");

-- Create "workflow_runs" table.
CREATE TABLE "workflow_runs" (
  "id" text NOT NULL,
  "repository_id" text NOT NULL,
  "github_run_id" integer NOT NULL,
  "github_workflow_id" integer NOT NULL,
  "workflow_name" text NOT NULL DEFAULT '',
  "run_number" integer NOT NULL DEFAULT 0,
  "event" text NOT NULL DEFAULT '',
  "head_branch" text NOT NULL DEFAULT '',
  "head_sha" text NOT NULL DEFAULT '',
  "status" text NOT NULL DEFAULT '',
  "conclusion" text NULL,
  "previous_conclusion" text NULL,
  "actor" text NOT NULL DEFAULT '',
  "run_attempt" integer NOT NULL DEFAULT 1,
  "html_url" text NOT NULL DEFAULT '',
  "run_started_at" datetime NULL,
  "run_updated_at" datetime NOT NULL,
  "run_completed_at" datetime NULL,
  "state_hash" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "workflowrun_repository_id_github_run_id" ON "workflow_runs" ("repository_id", "github_run_id");
CREATE INDEX "workflowrun_repository_id_github_workflow_id_head_branch" ON "workflow_runs" ("repository_id", "github_workflow_id", "head_branch");
CREATE INDEX "workflowrun_conclusion" ON "workflow_runs" ("conclusion");
CREATE INDEX "workflowrun_run_updated_at" ON "workflow_runs" ("run_updated_at");

-- Create "security_alerts" table.
CREATE TABLE "security_alerts" (
  "id" text NOT NULL,
  "repository_id" text NOT NULL,
  "alert_kind" text NOT NULL,
  "alert_number" integer NOT NULL,
  "state" text NOT NULL,
  "severity" text NOT NULL DEFAULT '',
  "rule_or_dependency" text NOT NULL DEFAULT '',
  "dismissed_reason" text NOT NULL DEFAULT '',
  "html_url" text NOT NULL DEFAULT '',
  "source_updated_at" datetime NOT NULL,
  "state_hash" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "securityalert_repository_id_alert_kind_alert_number" ON "security_alerts" ("repository_id", "alert_kind", "alert_number");
CREATE INDEX "securityalert_state" ON "security_alerts" ("state");
CREATE INDEX "securityalert_severity" ON "security_alerts" ("severity");
CREATE INDEX "securityalert_source_updated_at" ON "security_alerts" ("source_updated_at");

-- Create "events" table.
CREATE TABLE "events" (
  "id" text NOT NULL,
  "source" text NOT NULL,
  "kind" text NOT NULL,
  "action" text NOT NULL,
  "repository_id" text NULL,
  "subject_number" integer NULL,
  "title" text NOT NULL DEFAULT '',
  "severity" text NOT NULL DEFAULT '',
  "actor" text NOT NULL DEFAULT '',
  "workflow_run_id" integer NULL,
  "workflow_conclusion" text NOT NULL DEFAULT '',
  "occurred_at" datetime NOT NULL,
  "source_updated_at" datetime NULL,
  "html_url" text NOT NULL DEFAULT '',
  "payload_summary" json NULL,
  "suppress_notification" boolean NOT NULL DEFAULT FALSE,
  "dedupe_fingerprint" text NOT NULL,
  "state_hash" text NOT NULL DEFAULT '',
  "created_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "event_dedupe_fingerprint" ON "events" ("dedupe_fingerprint");
CREATE INDEX "event_repository_id_kind_occurred_at" ON "events" ("repository_id", "kind", "occurred_at");
CREATE INDEX "event_occurred_at" ON "events" ("occurred_at");
CREATE INDEX "event_created_at" ON "events" ("created_at");

-- Create "notification_channels" table.
CREATE TABLE "notification_channels" (
  "id" text NOT NULL,
  "channel_type" text NOT NULL,
  "name" text NOT NULL DEFAULT '',
  "enabled" boolean NOT NULL DEFAULT FALSE,
  "target" text NOT NULL DEFAULT '',
  "secret_envelope" text NOT NULL DEFAULT '',
  "allow_private" boolean NOT NULL DEFAULT FALSE,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "notificationchannel_channel_type_enabled" ON "notification_channels" ("channel_type", "enabled");

-- Create "notification_outbox" table.
CREATE TABLE "notification_outbox" (
  "id" text NOT NULL,
  "channel_id" text NOT NULL,
  "event_id" text NULL,
  "aggregate_key" text NOT NULL DEFAULT '',
  "idempotency_key" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "attempt_count" integer NOT NULL DEFAULT 0,
  "next_attempt_at" datetime NOT NULL,
  "locked_until" datetime NULL,
  "last_error_code" text NOT NULL DEFAULT '',
  "title" text NOT NULL DEFAULT '',
  "body_text" text NOT NULL,
  "body_json" json NULL,
  "parse_mode" text NOT NULL DEFAULT 'HTML',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "notificationoutbox_idempotency_key" ON "notification_outbox" ("idempotency_key");
CREATE INDEX "notificationoutbox_status_next_attempt_at" ON "notification_outbox" ("status", "next_attempt_at");
CREATE INDEX "notificationoutbox_channel_id_status" ON "notification_outbox" ("channel_id", "status");
CREATE INDEX "notificationoutbox_created_at" ON "notification_outbox" ("created_at");

-- Create "sync_cursors" table.
CREATE TABLE "sync_cursors" (
  "id" text NOT NULL,
  "repository_id" text NOT NULL,
  "resource" text NOT NULL,
  "cursor_value" text NOT NULL DEFAULT '',
  "etag" text NOT NULL DEFAULT '',
  "last_success_at" datetime NULL,
  "last_error_code" text NOT NULL DEFAULT '',
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "synccursor_repository_id_resource" ON "sync_cursors" ("repository_id", "resource");

-- Create "scheduled_jobs" table.
CREATE TABLE "scheduled_jobs" (
  "id" text NOT NULL,
  "job_type" text NOT NULL,
  "payload_json" text NOT NULL DEFAULT '{}',
  "status" text NOT NULL DEFAULT 'pending',
  "attempt_count" integer NOT NULL DEFAULT 0,
  "run_at" datetime NOT NULL,
  "locked_until" datetime NULL,
  "last_error_code" text NOT NULL DEFAULT '',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  "completed_at" datetime NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "scheduledjob_status_run_at" ON "scheduled_jobs" ("status", "run_at");
CREATE INDEX "scheduledjob_job_type_status" ON "scheduled_jobs" ("job_type", "status");
