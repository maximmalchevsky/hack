DROP TABLE IF EXISTS task_plan_slots;
DROP INDEX IF EXISTS tracker_tasks_priority_idx;
ALTER TABLE tracker_tasks
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS task_type,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS ai_estimated_hours,
    DROP COLUMN IF EXISTS ai_estimate_confidence;
