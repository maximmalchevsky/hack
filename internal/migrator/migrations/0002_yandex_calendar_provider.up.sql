-- Добавляем yandex_calendar в enum integration_provider.
-- Postgres ALTER TYPE ... ADD VALUE — атомарно (с 9.6+).
ALTER TYPE integration_provider ADD VALUE IF NOT EXISTS 'yandex_calendar';
