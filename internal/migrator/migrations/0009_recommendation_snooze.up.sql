-- Snooze для рекомендаций: «отложить на N дней, потом снова покажи».
-- Если snoozed_until > now() — рекомендация скрыта из списка.
-- NULL = не отложено.

ALTER TABLE recommendations
    ADD COLUMN snoozed_until timestamptz;

CREATE INDEX recommendations_snooze_idx
    ON recommendations (snoozed_until)
    WHERE snoozed_until IS NOT NULL;
