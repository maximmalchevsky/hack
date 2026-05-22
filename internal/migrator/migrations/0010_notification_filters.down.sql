ALTER TABLE users
    DROP COLUMN IF EXISTS notify_min_priority,
    DROP COLUMN IF EXISTS notify_kinds;
