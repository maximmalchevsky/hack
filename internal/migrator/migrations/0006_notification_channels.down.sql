DROP INDEX IF EXISTS users_telegram_chat_idx;

ALTER TABLE users
    DROP COLUMN IF EXISTS telegram_notifications,
    DROP COLUMN IF EXISTS telegram_chat_id,
    DROP COLUMN IF EXISTS email_notifications;
