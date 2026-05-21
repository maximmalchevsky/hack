-- Saved views — пользовательские пресеты фильтров на /analytics и /diagnostics.
-- Хранятся per-user. JSON-структура filters зависит от страницы:
--   analytics:   {tab, team_ids[], period}
--   diagnostics: {tab, departments[], roles[]}

CREATE TABLE view_presets (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    page       text NOT NULL CHECK (page IN ('analytics', 'diagnostics')),
    name       text NOT NULL,
    filters    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX view_presets_user_page_idx ON view_presets(user_id, page);
