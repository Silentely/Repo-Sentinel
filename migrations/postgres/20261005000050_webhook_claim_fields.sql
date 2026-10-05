ALTER TABLE "webhook_deliveries" ADD COLUMN "claim_token" character varying NOT NULL DEFAULT '';
ALTER TABLE "webhook_deliveries" ADD COLUMN "claim_version" bigint NOT NULL DEFAULT 0;
ALTER TABLE "webhook_deliveries" ADD COLUMN "claimed_by" character varying NOT NULL DEFAULT '';
ALTER TABLE "webhook_deliveries" ADD COLUMN "claimed_until" timestamptz NULL;
ALTER TABLE "webhook_deliveries" ADD COLUMN "attempt_count" integer NOT NULL DEFAULT 0;
ALTER TABLE "webhook_deliveries" ADD COLUMN "last_error_code" character varying NOT NULL DEFAULT '';
CREATE INDEX "webhook_deliveries_claim_check" ON "webhook_deliveries" ("status", "claimed_until");
