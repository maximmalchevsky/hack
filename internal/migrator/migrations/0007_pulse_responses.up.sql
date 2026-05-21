-- Pulse-check: короткий опрос «как ты себя чувствуешь?» раз в 2 недели.
-- Шкала 1..5 (от 😞 до 🤩), опциональный коммент.
-- Менеджер видит ответы каждого сотрудника команды (не анонимно).

CREATE TABLE pulse_responses (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    score       smallint NOT NULL CHECK (score BETWEEN 1 AND 5),
    comment     text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Быстрый доступ к последнему ответу сотрудника.
CREATE INDEX pulse_responses_employee_recent_idx
    ON pulse_responses (employee_id, created_at DESC);
