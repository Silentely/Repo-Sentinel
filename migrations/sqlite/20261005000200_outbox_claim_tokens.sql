-- Add claim_token column to notification_outbox
ALTER TABLE notification_outbox ADD COLUMN claim_token text;
