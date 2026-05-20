-- report_presets — сохранённые пользователем конфигурации отчётов
-- для страницы /reports/builder.
--
-- columns: ["Сотрудник","Email",...] — подмножество исходных headers пресета
-- filters: {"from":"2026-05-01","to":"2026-06-01","departments":["Platform"]}
CREATE TABLE report_presets (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    kind        text NOT NULL,    -- upcoming_vacations / stale_profiles / conflicts / all_employees
    columns     jsonb NOT NULL DEFAULT '[]'::jsonb,
    filters     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX report_presets_user_idx ON report_presets(user_id);
