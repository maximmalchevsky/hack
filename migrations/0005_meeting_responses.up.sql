-- meeting_responses — статус подтверждения каждого участника встречи.
-- Создаются автоматически при INSERT meeting_proposals:
--   * инициатор — сразу 'accepted' + yandex_pushed=true (он же создал)
--   * остальные участники команды — 'pending'
--
-- Участник через UI отвечает accept/decline; на accept (опц.) пушим ICS в его Yandex.
CREATE TYPE meeting_response_status AS ENUM ('pending', 'accepted', 'declined');

CREATE TABLE meeting_responses (
    meeting_id    uuid NOT NULL REFERENCES meeting_proposals(id) ON DELETE CASCADE,
    employee_id   uuid NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    status        meeting_response_status NOT NULL DEFAULT 'pending',
    yandex_pushed boolean NOT NULL DEFAULT false,
    responded_at  timestamptz,
    PRIMARY KEY (meeting_id, employee_id)
);

CREATE INDEX meeting_responses_emp_status_idx
    ON meeting_responses(employee_id, status);
