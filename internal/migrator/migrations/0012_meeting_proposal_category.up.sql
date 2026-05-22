-- Категория встречи, выбранная пользователем при создании. NULL = «определить
-- автоматически», тогда GigaChat решает при первом подсчёте «куда уходит время».
-- При accept участником эта же категория проставляется в его calendar_event.
ALTER TABLE meeting_proposals
    ADD COLUMN category text;
