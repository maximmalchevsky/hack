-- Гибкие настройки уведомлений: фильтр по типу и по минимальному приоритету.
-- Пустой массив notify_kinds = «все типы разрешены» (по умолчанию).
-- notify_min_priority = 'low' = «всё» (по умолчанию).

ALTER TABLE users
    ADD COLUMN notify_kinds        text[] NOT NULL DEFAULT '{}',
    ADD COLUMN notify_min_priority text   NOT NULL DEFAULT 'low'
        CHECK (notify_min_priority IN ('low', 'medium', 'high'));
