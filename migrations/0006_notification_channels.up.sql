-- Каналы уведомлений в users.
--   email_notifications     — слать ли in-app нотификации копией на email
--   telegram_chat_id        — привязанный chat_id (NULL если не привязан)
--   telegram_notifications  — флаг разрешения для telegram (если chat_id есть)
ALTER TABLE users
    ADD COLUMN email_notifications    boolean NOT NULL DEFAULT true,
    ADD COLUMN telegram_chat_id       text,
    ADD COLUMN telegram_notifications boolean NOT NULL DEFAULT true;

CREATE INDEX users_telegram_chat_idx ON users(telegram_chat_id)
    WHERE telegram_chat_id IS NOT NULL;
