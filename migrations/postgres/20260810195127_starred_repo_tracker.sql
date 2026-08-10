-- star 仓库 release 追踪表
-- Create "starred_repo_trackers" table.
CREATE TABLE "starred_repo_trackers" (
  "id" text NOT NULL,
  "full_name" text NOT NULL,
  "state" text NOT NULL,
  "etag" text NULL,
  "last_release_id" bigint NOT NULL DEFAULT 0,
  "last_release_tag" text NULL,
  "last_release_published_at" timestamptz NULL,
  "no_release_since" timestamptz NULL,
  "no_release_recheck_at" timestamptz NULL,
  "first_seen_at" timestamptz NOT NULL,
  "last_poll_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "starredrepotracker_full_name" ON "starred_repo_trackers" ("full_name");
CREATE INDEX "starredrepotracker_state_last_poll_at" ON "starred_repo_trackers" ("state", "last_poll_at");
