-- Расширение tracker_tasks для интеграции с Jira и планировщика времени.
-- Добавляем поля, которых не хватало в 0001_init:
--   priority      — Highest/High/Medium/Low/Lowest (Jira priority.name)
--   task_type     — Story/Task/Bug/Epic/Subtask (Jira issuetype.name)
--   description   — для AI-оценки (берётся из Jira description)
--   ai_estimated_hours — оценка GigaChat'ом, отдельно от ручной estimated_hours
--   ai_estimate_confidence — 0..1, уверенность модели

ALTER TABLE tracker_tasks
    ADD COLUMN priority text,
    ADD COLUMN task_type text,
    ADD COLUMN description text,
    ADD COLUMN ai_estimated_hours numeric(6, 2),
    ADD COLUMN ai_estimate_confidence numeric(3, 2);

CREATE INDEX tracker_tasks_priority_idx
    ON tracker_tasks (employee_id, priority)
    WHERE priority IS NOT NULL;

-- task_plan_slots — рассчитанные слоты времени задачи по дням.
-- Заполняется TaskPlannerService при replan'е (полный пересчёт: DELETE+INSERT
-- всех слотов сотрудника). Используется UI /tasks (Gantt) и виджетом на
-- /dashboard «запланировано сегодня».
CREATE TABLE task_plan_slots (
    id           uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id      uuid NOT NULL REFERENCES tracker_tasks(id) ON DELETE CASCADE,
    employee_id  uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    date         date NOT NULL,
    hours        numeric(4, 2) NOT NULL CHECK (hours > 0),
    computed_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX task_plan_slots_emp_date_idx ON task_plan_slots (employee_id, date);
CREATE UNIQUE INDEX task_plan_slots_uniq ON task_plan_slots (task_id, date);
