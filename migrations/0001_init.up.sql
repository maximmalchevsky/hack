-- =====================================================================
-- WorkTime Sync — initial schema
-- =====================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "btree_gist";

-- ---------------------------------------------------------------------
-- ENUMs
-- ---------------------------------------------------------------------

CREATE TYPE user_role AS ENUM (
    'admin', 'employee', 'manager', 'hr', 'pm', 'analyst'
);

CREATE TYPE work_format AS ENUM (
    'office', 'remote', 'hybrid'
);

CREATE TYPE exception_kind AS ENUM (
    'vacation', 'sick_leave', 'business_trip', 'personal_hours', 'custom'
);

CREATE TYPE integration_provider AS ENUM (
    'ical', 'caldav', 'google_calendar', 'ms365', 'jira', 'yandex_tracker'
);

CREATE TYPE integration_status AS ENUM (
    'connected', 'error', 'disabled', 'pending'
);

CREATE TYPE event_status AS ENUM (
    'confirmed', 'tentative', 'cancelled'
);

CREATE TYPE recommendation_kind AS ENUM (
    'update_profile', 'move_meeting', 'check_tz', 'no_new_meetings',
    'reduce_load', 'confirm_schedule', 'check_hr_data', 'change_meeting_window',
    'custom'
);

CREATE TYPE recommendation_status AS ENUM (
    'new', 'seen', 'applied', 'dismissed'
);

CREATE TYPE recommendation_priority AS ENUM (
    'low', 'medium', 'high', 'critical'
);

CREATE TYPE ai_message_role AS ENUM (
    'user', 'assistant', 'system'
);

-- ---------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    email           text NOT NULL UNIQUE,
    password_hash   text NOT NULL,
    role            user_role NOT NULL DEFAULT 'employee',
    full_name       text NOT NULL,
    timezone        text NOT NULL DEFAULT 'Europe/Moscow',
    locale          text NOT NULL DEFAULT 'ru',
    avatar_url      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX users_role_idx ON users(role);

-- ---------------------------------------------------------------------
-- employees
-- ---------------------------------------------------------------------

CREATE TABLE employees (
    id                      uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id                 uuid NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    department              text,
    position                text,
    hr_work_format          work_format,
    hire_date               date,
    last_profile_update_at  timestamptz,
    last_confirmed_at       timestamptz,
    manager_id              uuid REFERENCES employees(id) ON DELETE SET NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX employees_manager_idx ON employees(manager_id);
CREATE INDEX employees_department_idx ON employees(department);

-- ---------------------------------------------------------------------
-- teams
-- ---------------------------------------------------------------------

CREATE TABLE teams (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        text NOT NULL,
    owner_id    uuid REFERENCES employees(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id     uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    joined_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, employee_id)
);

CREATE INDEX team_members_employee_idx ON team_members(employee_id);

-- ---------------------------------------------------------------------
-- work_profiles (temporal: valid_from / valid_to)
-- Активная запись — valid_to IS NULL. История — остальные.
-- ---------------------------------------------------------------------

CREATE TABLE work_profiles (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id     uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    valid_from      timestamptz NOT NULL DEFAULT now(),
    valid_to        timestamptz,
    days_of_week    jsonb NOT NULL,
    -- формат: {"mon":{"start":"09:00","end":"18:00"}, "tue":{...}, ..., "sun": null }
    timezone        text NOT NULL DEFAULT 'Europe/Moscow',
    work_format     work_format NOT NULL DEFAULT 'office',
    source          text NOT NULL DEFAULT 'manual',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX work_profiles_employee_idx ON work_profiles(employee_id);
CREATE INDEX work_profiles_active_idx ON work_profiles(employee_id) WHERE valid_to IS NULL;
CREATE UNIQUE INDEX work_profiles_active_uniq ON work_profiles(employee_id) WHERE valid_to IS NULL;

-- ---------------------------------------------------------------------
-- time_exceptions
-- ---------------------------------------------------------------------

CREATE TABLE time_exceptions (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    kind        exception_kind NOT NULL,
    start_at    timestamptz NOT NULL,
    end_at      timestamptz NOT NULL,
    comment     text,
    source      text NOT NULL DEFAULT 'manual',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at > start_at)
);

CREATE INDEX time_exceptions_employee_idx ON time_exceptions(employee_id, start_at, end_at);
CREATE INDEX time_exceptions_kind_idx ON time_exceptions(kind);

-- ---------------------------------------------------------------------
-- integrations
-- ---------------------------------------------------------------------

CREATE TABLE integrations (
    id                  uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id         uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    provider            integration_provider NOT NULL,
    account_email       text,
    account_label       text,
    -- зашифрованные AES-256-GCM значения (base64)
    access_token_enc    text,
    refresh_token_enc   text,
    expires_at          timestamptz,
    status              integration_status NOT NULL DEFAULT 'pending',
    last_sync_at        timestamptz,
    last_error          text,
    webhook_sub_id      text,
    config              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX integrations_employee_idx ON integrations(employee_id);
CREATE INDEX integrations_provider_status_idx ON integrations(provider, status);
CREATE INDEX integrations_expires_idx ON integrations(expires_at) WHERE expires_at IS NOT NULL;

-- ---------------------------------------------------------------------
-- calendar_events
-- ---------------------------------------------------------------------

CREATE TABLE calendar_events (
    id                  uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id         uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    integration_id      uuid REFERENCES integrations(id) ON DELETE SET NULL,
    source_event_id     text NOT NULL,
    title               text,
    description         text,
    start_at            timestamptz NOT NULL,
    end_at              timestamptz NOT NULL,
    timezone            text,
    is_recurring        boolean NOT NULL DEFAULT false,
    rrule               text,
    recurrence_root_id  uuid REFERENCES calendar_events(id) ON DELETE SET NULL,
    attendees_count     int,
    organizer           text,
    status              event_status NOT NULL DEFAULT 'confirmed',
    is_excluded         boolean NOT NULL DEFAULT false,
    raw                 jsonb,
    fetched_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at > start_at)
);

CREATE INDEX calendar_events_employee_period_idx ON calendar_events(employee_id, start_at, end_at)
    WHERE is_excluded = false;
CREATE UNIQUE INDEX calendar_events_source_uniq ON calendar_events(integration_id, source_event_id)
    WHERE integration_id IS NOT NULL;
CREATE INDEX calendar_events_recurrence_idx ON calendar_events(recurrence_root_id);

-- ---------------------------------------------------------------------
-- tracker_tasks
-- ---------------------------------------------------------------------

CREATE TABLE tracker_tasks (
    id                  uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id         uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    integration_id      uuid REFERENCES integrations(id) ON DELETE SET NULL,
    source_task_id      text NOT NULL,
    title               text,
    status              text,
    due_at              timestamptz,
    estimated_hours     numeric(6, 2),
    actual_hours        numeric(6, 2),
    raw                 jsonb,
    fetched_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tracker_tasks_employee_due_idx ON tracker_tasks(employee_id, due_at);
CREATE UNIQUE INDEX tracker_tasks_source_uniq ON tracker_tasks(integration_id, source_task_id)
    WHERE integration_id IS NOT NULL;

-- ---------------------------------------------------------------------
-- metrics_snapshots
-- ---------------------------------------------------------------------

CREATE TABLE metrics_snapshots (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id     uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    computed_at     timestamptz NOT NULL DEFAULT now(),
    period_start    timestamptz NOT NULL,
    period_end      timestamptz NOT NULL,
    freshness_a     numeric(5, 4) NOT NULL,
    conflicts_c     numeric(5, 4) NOT NULL,
    load_l          numeric(5, 4) NOT NULL,
    tz_drift_z      numeric(5, 4) NOT NULL,
    hr_calendar_h   numeric(5, 4) NOT NULL,
    risk_r          numeric(5, 4) NOT NULL,
    details         jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX metrics_snapshots_employee_idx ON metrics_snapshots(employee_id, computed_at DESC);

-- ---------------------------------------------------------------------
-- team_availability_cache
-- ---------------------------------------------------------------------

CREATE TABLE team_availability_cache (
    team_id     uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    day         date NOT NULL,
    slots       jsonb NOT NULL,
    computed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, day)
);

-- ---------------------------------------------------------------------
-- recommendations
-- ---------------------------------------------------------------------

CREATE TABLE recommendations (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id     uuid REFERENCES employees(id) ON DELETE CASCADE,
    team_id         uuid REFERENCES teams(id) ON DELETE CASCADE,
    kind            recommendation_kind NOT NULL,
    priority        recommendation_priority NOT NULL DEFAULT 'medium',
    title           text NOT NULL,
    explanation     text NOT NULL,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    status          recommendation_status NOT NULL DEFAULT 'new',
    generated_by    text NOT NULL DEFAULT 'rule',
    ai_evidence     jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (employee_id IS NOT NULL OR team_id IS NOT NULL)
);

CREATE INDEX recommendations_employee_idx ON recommendations(employee_id) WHERE employee_id IS NOT NULL;
CREATE INDEX recommendations_team_idx ON recommendations(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX recommendations_status_priority_idx ON recommendations(status, priority);

-- ---------------------------------------------------------------------
-- notifications
-- ---------------------------------------------------------------------

CREATE TABLE notifications (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    title       text NOT NULL,
    body        text,
    link        text,
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_unread_idx ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE TABLE notification_rules (
    id          uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    kind        text NOT NULL,
    params      jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- ai_conversations / ai_messages
-- ---------------------------------------------------------------------

CREATE TABLE ai_conversations (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           text,
    started_at      timestamptz NOT NULL DEFAULT now(),
    last_message_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ai_conversations_user_idx ON ai_conversations(user_id, last_message_at DESC);

CREATE TABLE ai_messages (
    id                  uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id     uuid NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role                ai_message_role NOT NULL,
    content             text NOT NULL,
    tool_calls          jsonb,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ai_messages_conversation_idx ON ai_messages(conversation_id, created_at);

-- ---------------------------------------------------------------------
-- audit_log
-- ---------------------------------------------------------------------

CREATE TABLE audit_log (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    action          text NOT NULL,
    entity          text NOT NULL,
    entity_id       uuid,
    before          jsonb,
    after           jsonb,
    ip              inet,
    user_agent      text,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_entity_idx ON audit_log(entity, entity_id, created_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log(actor_user_id, created_at DESC);

-- ---------------------------------------------------------------------
-- webhook_inbox
-- ---------------------------------------------------------------------

CREATE TABLE webhook_inbox (
    id              uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider        integration_provider NOT NULL,
    signature_ok    boolean NOT NULL DEFAULT false,
    payload         jsonb NOT NULL,
    processed_at    timestamptz,
    error           text,
    received_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX webhook_inbox_unprocessed_idx ON webhook_inbox(received_at)
    WHERE processed_at IS NULL;

-- ---------------------------------------------------------------------
-- analytics_weights — конфигурируемые веса для риска R
-- ---------------------------------------------------------------------

CREATE TABLE analytics_weights (
    id          int PRIMARY KEY DEFAULT 1,
    w1          numeric(4, 3) NOT NULL DEFAULT 0.300,
    w2          numeric(4, 3) NOT NULL DEFAULT 0.250,
    w3          numeric(4, 3) NOT NULL DEFAULT 0.200,
    w4          numeric(4, 3) NOT NULL DEFAULT 0.150,
    w5          numeric(4, 3) NOT NULL DEFAULT 0.100,
    freshness_d_days int NOT NULL DEFAULT 90,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (id = 1)
);

INSERT INTO analytics_weights (id) VALUES (1) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------
-- updated_at triggers
-- ---------------------------------------------------------------------

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER employees_updated_at BEFORE UPDATE ON employees
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER teams_updated_at BEFORE UPDATE ON teams
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER integrations_updated_at BEFORE UPDATE ON integrations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER recommendations_updated_at BEFORE UPDATE ON recommendations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
