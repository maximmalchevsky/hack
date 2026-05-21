-- Откатить ADD VALUE для enum в Postgres нельзя без пересоздания типа.
-- В rollback оставляем no-op, чтобы не падать.
SELECT 1;
