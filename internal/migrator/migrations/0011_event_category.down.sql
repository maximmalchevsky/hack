DROP INDEX IF EXISTS calendar_events_category_idx;
ALTER TABLE calendar_events DROP COLUMN IF EXISTS category;
