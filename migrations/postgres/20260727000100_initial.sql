-- Create "admin_accounts" table.
CREATE TABLE "admin_accounts" (
  "id" character varying(255) NOT NULL,
  "username" character varying(255) NOT NULL,
  "username_normalized" character varying(255) NOT NULL,
  "password_hash" character varying(255) NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "password_changed_at" timestamptz NOT NULL,
  "singleton_slot" bigint NOT NULL DEFAULT 1,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "adminaccount_username_normalized" ON "admin_accounts" ("username_normalized");
CREATE UNIQUE INDEX "adminaccount_singleton_slot" ON "admin_accounts" ("singleton_slot");

-- Create "admin_sessions" table.
CREATE TABLE "admin_sessions" (
  "id" character varying(255) NOT NULL,
  "token_hash" character varying(255) NOT NULL,
  "csrf_hash" character varying(255) NOT NULL,
  "created_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "last_seen_at" timestamptz NOT NULL,
  "ip_address" character varying(255) NOT NULL,
  "user_agent" character varying(255) NOT NULL,
  "admin_id" character varying(255) NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_sessions_admin_accounts_sessions"
    FOREIGN KEY ("admin_id") REFERENCES "admin_accounts" ("id") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "adminsession_token_hash" ON "admin_sessions" ("token_hash");
CREATE INDEX "adminsession_expires_at" ON "admin_sessions" ("expires_at");

-- Create "audit_logs" table.
CREATE TABLE "audit_logs" (
  "id" character varying(255) NOT NULL,
  "action" character varying(255) NOT NULL,
  "actor_type" character varying(255) NOT NULL,
  "actor_id" character varying(255) NOT NULL,
  "target_type" character varying(255) NOT NULL,
  "target_id" character varying(255) NOT NULL,
  "metadata_json" jsonb NOT NULL,
  "ip_address" character varying(255) NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "auditlog_created_at" ON "audit_logs" ("created_at");
CREATE INDEX "auditlog_action" ON "audit_logs" ("action");

-- Create "system_settings" table.
CREATE TABLE "system_settings" (
  "id" character varying(255) NOT NULL,
  "key" character varying(255) NOT NULL,
  "value_json" jsonb NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "updated_by" character varying(255) NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "systemsetting_key" ON "system_settings" ("key");
