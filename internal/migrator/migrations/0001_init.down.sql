-- Откат initial schema

DROP TRIGGER IF EXISTS recommendations_updated_at ON recommendations;
DROP TRIGGER IF EXISTS integrations_updated_at ON integrations;
DROP TRIGGER IF EXISTS teams_updated_at ON teams;
DROP TRIGGER IF EXISTS employees_updated_at ON employees;
DROP TRIGGER IF EXISTS users_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at;

DROP TABLE IF EXISTS analytics_weights;
DROP TABLE IF EXISTS webhook_inbox;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS notification_rules;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS recommendations;
DROP TABLE IF EXISTS team_availability_cache;
DROP TABLE IF EXISTS metrics_snapshots;
DROP TABLE IF EXISTS tracker_tasks;
DROP TABLE IF EXISTS calendar_events;
DROP TABLE IF EXISTS integrations;
DROP TABLE IF EXISTS time_exceptions;
DROP TABLE IF EXISTS work_profiles;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS ai_message_role;
DROP TYPE IF EXISTS recommendation_priority;
DROP TYPE IF EXISTS recommendation_status;
DROP TYPE IF EXISTS recommendation_kind;
DROP TYPE IF EXISTS event_status;
DROP TYPE IF EXISTS integration_status;
DROP TYPE IF EXISTS integration_provider;
DROP TYPE IF EXISTS exception_kind;
DROP TYPE IF EXISTS work_format;
DROP TYPE IF EXISTS user_role;
