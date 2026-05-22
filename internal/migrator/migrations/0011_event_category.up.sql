-- Тип/категория встречи: либо выбран пользователем при создании/редактировании,
-- либо проставлен GigaChat'ом по title (lazy + кэширование в этой же колонке).
-- NULL = ещё не классифицировано.

ALTER TABLE calendar_events
    ADD COLUMN category text;

CREATE INDEX calendar_events_category_idx
    ON calendar_events (category)
    WHERE category IS NOT NULL;
