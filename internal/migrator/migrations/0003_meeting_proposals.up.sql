-- meeting_proposals — встречи, созданные через MeetingProposalService.
-- Нужны для истории, отмены и идемпотентности (чтобы не дублировать пуш).
CREATE TABLE meeting_proposals (
    id               uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    initiator_user   uuid REFERENCES users(id) ON DELETE SET NULL,
    initiator_emp    uuid REFERENCES employees(id) ON DELETE SET NULL,
    team_id          uuid REFERENCES teams(id) ON DELETE SET NULL,
    title            text NOT NULL,
    start_at         timestamptz NOT NULL,
    end_at           timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    cancelled_at     timestamptz,
    cancelled_by     uuid REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX meeting_proposals_initiator_emp_idx ON meeting_proposals(initiator_emp);
CREATE INDEX meeting_proposals_team_idx          ON meeting_proposals(team_id);
CREATE INDEX meeting_proposals_active_idx        ON meeting_proposals(start_at)
    WHERE cancelled_at IS NULL;

-- meeting_pushes — куда именно мы протолкнули встречу (в чей календарь).
-- Нужно, чтобы при отмене DELETE отправить во ВСЕ затронутые календари.
CREATE TABLE meeting_pushes (
    id                uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    meeting_id        uuid NOT NULL REFERENCES meeting_proposals(id) ON DELETE CASCADE,
    employee_id       uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    integration_id    uuid REFERENCES integrations(id) ON DELETE SET NULL,
    provider          text NOT NULL,                 -- 'yandex_calendar' и т.д.
    source_event_uid  text NOT NULL,                 -- UID из ICS (то что вернул CalDAV)
    calendar_path     text,                          -- путь к календарю на сервере провайдера
    created_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz,                   -- успешно удалили из календаря провайдера
    delete_error      text                           -- если DELETE упал — пишем сюда (best-effort)
);

CREATE INDEX meeting_pushes_meeting_idx  ON meeting_pushes(meeting_id);
CREATE INDEX meeting_pushes_employee_idx ON meeting_pushes(employee_id);
