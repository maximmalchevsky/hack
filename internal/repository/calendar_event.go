package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

type CalendarEventRepo struct {
	pool *pgxpool.Pool
}

func NewCalendarEventRepo(pool *pgxpool.Pool) *CalendarEventRepo {
	return &CalendarEventRepo{pool: pool}
}

type UpsertEventInput struct {
	EmployeeID     uuid.UUID
	IntegrationID  *uuid.UUID
	SourceEventID  string
	Title          string
	Description    string
	StartAt        time.Time
	EndAt          time.Time
	Timezone       string
	IsRecurring    bool
	RRule          string
	Organizer      string
	AttendeesCount int
	Status         domain.EventStatus
}

// Upsert — INSERT…ON CONFLICT по (integration_id, source_event_id).
func (r *CalendarEventRepo) Upsert(ctx context.Context, in UpsertEventInput) (*domain.CalendarEvent, error) {
	if !in.EndAt.After(in.StartAt) {
		return nil, fmt.Errorf("end_at must be after start_at")
	}
	if in.Status == "" {
		in.Status = domain.EventConfirmed
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO calendar_events
			(employee_id, integration_id, source_event_id, title, description,
			 start_at, end_at, timezone, is_recurring, rrule, organizer,
			 attendees_count, status, fetched_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''),
		        $6, $7, NULLIF($8, ''), $9, NULLIF($10, ''), NULLIF($11, ''),
		        $12, $13, now())
		ON CONFLICT (integration_id, source_event_id) WHERE integration_id IS NOT NULL
		DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			start_at = EXCLUDED.start_at,
			end_at = EXCLUDED.end_at,
			timezone = EXCLUDED.timezone,
			is_recurring = EXCLUDED.is_recurring,
			rrule = EXCLUDED.rrule,
			organizer = EXCLUDED.organizer,
			attendees_count = EXCLUDED.attendees_count,
			status = EXCLUDED.status,
			fetched_at = now()
		RETURNING id, employee_id, integration_id, source_event_id,
		          COALESCE(title, ''), COALESCE(description, ''),
		          start_at, end_at, COALESCE(timezone, ''),
		          is_recurring, COALESCE(rrule, ''), recurrence_root_id,
		          COALESCE(attendees_count, 0), COALESCE(organizer, ''),
		          status, is_excluded, fetched_at
	`,
		in.EmployeeID, in.IntegrationID, in.SourceEventID, in.Title, in.Description,
		in.StartAt.UTC(), in.EndAt.UTC(), in.Timezone, in.IsRecurring, in.RRule,
		in.Organizer, in.AttendeesCount, string(in.Status))

	return scanCalendarEvent(row)
}

type ListEventsFilter struct {
	EmployeeID uuid.UUID
	From       time.Time
	To         time.Time
}

func (r *CalendarEventRepo) List(ctx context.Context, f ListEventsFilter) ([]domain.CalendarEvent, error) {
	rows, err := r.pool.Query(ctx, `
		`+calendarEventCols+`
		FROM calendar_events
		WHERE employee_id = $1
		  AND is_excluded = false
		  AND (
		      $2::timestamptz IS NULL OR $3::timestamptz IS NULL
		      OR (start_at < $3 AND end_at > $2)
		  )
		ORDER BY start_at
	`, f.EmployeeID, nullTime(f.From), nullTime(f.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CalendarEvent
	for rows.Next() {
		e, err := scanCalendarEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *CalendarEventRepo) Exclude(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE calendar_events SET is_excluded = true WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *CalendarEventRepo) Count(ctx context.Context, employeeID uuid.UUID, from, to time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM calendar_events
		WHERE employee_id = $1 AND is_excluded = false
		  AND start_at >= $2 AND end_at <= $3
	`, employeeID, from.UTC(), to.UTC()).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// --- helpers ---

const calendarEventCols = `
	SELECT id, employee_id, integration_id, source_event_id,
	       COALESCE(title, ''), COALESCE(description, ''),
	       start_at, end_at, COALESCE(timezone, ''),
	       is_recurring, COALESCE(rrule, ''), recurrence_root_id,
	       COALESCE(attendees_count, 0), COALESCE(organizer, ''),
	       status, is_excluded, fetched_at
`

func scanCalendarEvent(s rowScanner) (*domain.CalendarEvent, error) {
	var (
		e      domain.CalendarEvent
		status string
	)
	if err := s.Scan(
		&e.ID, &e.EmployeeID, &e.IntegrationID, &e.SourceEventID,
		&e.Title, &e.Description, &e.StartAt, &e.EndAt, &e.Timezone,
		&e.IsRecurring, &e.RRule, &e.RecurrenceRootID,
		&e.AttendeesCount, &e.Organizer, &status, &e.IsExcluded, &e.FetchedAt,
	); err != nil {
		return nil, err
	}
	e.Status = domain.EventStatus(status)
	return &e, nil
}
