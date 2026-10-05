-- Channel routing and outbox suppression audit columns
ALTER TABLE "notification_channels" ADD COLUMN "repo_pattern" character varying NOT NULL DEFAULT '';
ALTER TABLE "notification_channels" ADD COLUMN "branch_filter" character varying NOT NULL DEFAULT '';
ALTER TABLE "notification_channels" ADD COLUMN "min_severity" character varying NOT NULL DEFAULT 'low';
ALTER TABLE "notification_outbox" ADD COLUMN "suppressed_reason" character varying NOT NULL DEFAULT '';
