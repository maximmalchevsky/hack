DROP INDEX IF EXISTS recommendations_snooze_idx;
ALTER TABLE recommendations DROP COLUMN IF EXISTS snoozed_until;
