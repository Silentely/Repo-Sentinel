CREATE TABLE "system_leases" (
  "id" character varying PRIMARY KEY NOT NULL,
  "task_name" character varying NOT NULL,
  "holder_id" character varying NOT NULL DEFAULT '',
  "acquired_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "fencing_token" bigint NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX "system_leases_task_name" ON "system_leases" ("task_name");
CREATE INDEX "system_leases_expires_at" ON "system_leases" ("expires_at");
