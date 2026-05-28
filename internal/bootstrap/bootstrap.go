// Package bootstrap — первичная инициализация системы при старте API.
//
// Что делает:
//  1. Если admin'а нет — создаёт со случайным паролем и пишет креды в лог ОДИН РАЗ
//  2. Создаёт демо-сотрудников (если их нет) с фиксированным паролем для удобства демо
//  3. Создаёт демо-команды и состав
//
// Идемпотентно — повторный запуск пропускает существующих пользователей.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"worktimesync/pkg/auth"
)

// Enqueuer — минимальный интерфейс для постановки фоновых задач после seed'а.
// Bootstrap не должен зависеть от пакета workers (циклический импорт), поэтому
// принимаем абстракцию. Может быть nil — тогда задачи не ставятся.
type Enqueuer interface {
	EnqueueMetricsRecompute(employeeID uuid.UUID) error
	EnqueueAIRecommend(employeeID uuid.UUID) error
}

// Run — выполняет bootstrap-логику. Безопасно вызывать при каждом старте.
// Если enq != nil, ставит каждому сотруднику metrics:recompute и (только при
// первичном seed'е событий) ai:recommend.
//
// metrics:recompute дёргаем ВСЕГДА: metrics_snapshots должен быть тёплый сразу
// после старта api, иначе /analytics показывает нули, пока не сработает первый
// пересчёт (а он триггерится только изменениями профиля/событий/исключений).
//
// ai:recommend ставим только если события реально засеялись — иначе на каждый
// рестарт будем дёргать GigaChat впустую.
func Run(ctx context.Context, db *pgxpool.Pool, enq Enqueuer, log zerolog.Logger) error {
	if err := ensureAdmin(ctx, db, log); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if err := ensureDemoEmployees(ctx, db, log); err != nil {
		return fmt.Errorf("bootstrap demo: %w", err)
	}
	if err := ensureDemoTeams(ctx, db, log); err != nil {
		return fmt.Errorf("bootstrap teams: %w", err)
	}
	created, err := ensureDemoEvents(ctx, db, log)
	if err != nil {
		return fmt.Errorf("bootstrap events: %w", err)
	}
	if err := backfillMeetingEvents(ctx, db, log); err != nil {
		// best-effort — без backfill старые встречи останутся невидимы,
		// но это не повод валить весь bootstrap.
		log.Warn().Err(err).Msg("bootstrap: backfill meeting events failed")
	}
	if enq != nil {
		enqueueAllEmployees(ctx, db, enq, log, created)
	}
	return nil
}

// backfillMeetingEvents — для всех активных (не cancelled) meeting_proposals,
// у которых ещё нет calendar_event у инициатора, создаёт его. Идемпотентно
// (NOT EXISTS-фильтр). Нужен потому, что раньше Propose() писал calendar_event
// только при успешном push в Yandex; встречи без Yandex-интеграции
// у инициатора были невидимы в /dashboard, /workload, find-window.
func backfillMeetingEvents(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) error {
	// 0. Чистим дубли: если встреча запушена в Yandex (есть meeting_pushes),
	// то она уже попадает в calendar_events через CalDAV-sync (с integration_id).
	// Наш native-дубль (integration_id IS NULL, source 'meeting-…') тогда лишний —
	// удаляем его, чтобы в /dashboard и /workload не было двух одинаковых встреч.
	if tag, err := db.Exec(ctx, `
		DELETE FROM calendar_events ce
		WHERE ce.integration_id IS NULL
		  AND ce.source_event_id LIKE 'meeting-%'
		  AND EXISTS (
		      SELECT 1 FROM meeting_pushes mpu
		      WHERE mpu.deleted_at IS NULL
		        AND ce.source_event_id =
		            'meeting-' || mpu.meeting_id::text || '-' || mpu.employee_id::text
		  )
	`); err != nil {
		return err
	} else if n := tag.RowsAffected(); n > 0 {
		log.Info().Int64("count", n).Msg("bootstrap: removed duplicate native meeting events (have Yandex push)")
	}

	// 1. Создаём native calendar_event для встреч инициатора, у которых нет
	// НИ native-события, НИ Yandex-push'а. Без второго условия мы плодили бы
	// дубль рядом с Yandex-версией.
	tag, err := db.Exec(ctx, `
		INSERT INTO calendar_events
			(employee_id, integration_id, source_event_id, title, description,
			 start_at, end_at, status, category, fetched_at)
		SELECT mp.initiator_emp,
		       NULL,
		       'meeting-' || mp.id::text || '-' || mp.initiator_emp::text,
		       mp.title,
		       NULL,
		       mp.start_at,
		       mp.end_at,
		       'confirmed',
		       NULLIF(mp.category, ''),
		       now()
		FROM meeting_proposals mp
		WHERE mp.cancelled_at IS NULL
		  AND mp.initiator_emp IS NOT NULL
		  AND mp.end_at > now()
		  AND NOT EXISTS (
		      SELECT 1 FROM calendar_events ce
		      WHERE ce.integration_id IS NULL
		        AND ce.source_event_id = 'meeting-' || mp.id::text || '-' || mp.initiator_emp::text
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM meeting_pushes mpu
		      WHERE mpu.meeting_id = mp.id
		        AND mpu.employee_id = mp.initiator_emp
		        AND mpu.deleted_at IS NULL
		  )
	`)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Info().Int64("count", n).Msg("bootstrap: backfilled meeting calendar_events")
	}
	return nil
}

// enqueueAllEmployees — ставит metrics:recompute всем сотрудникам всегда, а
// ai:recommend — только если в этом запуске засеялись события.
func enqueueAllEmployees(ctx context.Context, db *pgxpool.Pool, enq Enqueuer, log zerolog.Logger, withRecommend bool) {
	rows, err := db.Query(ctx, `SELECT id FROM employees ORDER BY id`)
	if err != nil {
		log.Warn().Err(err).Msg("bootstrap: list employees for enqueue")
		return
	}
	defer rows.Close()
	queued := 0
	for rows.Next() {
		var empID uuid.UUID
		if err := rows.Scan(&empID); err != nil {
			continue
		}
		_ = enq.EnqueueMetricsRecompute(empID)
		if withRecommend {
			_ = enq.EnqueueAIRecommend(empID)
		}
		queued++
	}
	log.Info().Int("employees", queued).Bool("with_recommend", withRecommend).
		Msg("bootstrap: enqueued metrics recompute for all")
}

const adminEmail = "admin@worktime.local"

func ensureAdmin(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) error {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`,
		adminEmail).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	password, err := randomPassword(20)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, full_name, timezone)
		VALUES ($1, $2, 'admin', 'Александр Черемисов', 'Europe/Moscow')
		RETURNING id
	`, adminEmail, hash).Scan(&userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO employees (user_id, department, position, hr_work_format,
		                       hire_date, last_profile_update_at, last_confirmed_at)
		VALUES ($1, 'Operations', 'Системный администратор', 'office',
		        $2, $3, $3)
	`, userID, time.Now().AddDate(-2, 0, 0), time.Now().AddDate(0, 0, -5)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// ОДНО РАЗ ИЗ ВСЕХ ЗАПУСКОВ — выводим креды.
	log.Warn().Msg("==================== ADMIN CREATED (one-time) ====================")
	log.Warn().Str("email", adminEmail).Str("password", password).
		Msg("Сохрани этот пароль. Он больше нигде не появится.")
	log.Warn().Msg("==================================================================")
	return nil
}

func randomPassword(length int) (string, error) {
	// Алфавит без визуально неоднозначных символов (0/O, 1/l/I, etc).
	alphabet := "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%&*-_=+"
	out := make([]byte, length)
	bn := big.NewInt(int64(len(alphabet)))
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, bn)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}

// --- демо-сотрудники ---

type demoPerson struct {
	Email      string
	FullName   string
	Role       string
	Department string
	Position   string
	HRFormat   string
	Timezone   string
	DaysShift  int
	WorkFormat string
	HoursStart string
	HoursEnd   string
	Exceptions []demoException
}

type demoException struct {
	Kind    string
	StartIn int
	EndIn   int
	Comment string
}

const demoPassword = "qwerty12345"

func demoPeople() []demoPerson {
	return []demoPerson{
		{Email: "anna@worktime.local", FullName: "Максим Малчевский", Role: "employee",
			Department: "Platform", Position: "Backend-инженер", HRFormat: "hybrid",
			Timezone: "Europe/Moscow", DaysShift: 3, WorkFormat: "hybrid",
			HoursStart: "10:00", HoursEnd: "19:00"},
		{Email: "igor@worktime.local", FullName: "Игорь Климов", Role: "manager",
			Department: "Platform", Position: "Руководитель платформы", HRFormat: "office",
			Timezone: "Europe/Moscow", DaysShift: 10, WorkFormat: "office",
			HoursStart: "09:00", HoursEnd: "18:00",
			Exceptions: []demoException{{Kind: "vacation", StartIn: 14, EndIn: 28, Comment: "Отпуск"}}},
		{Email: "maria@worktime.local", FullName: "Степан Жаров", Role: "pm",
			Department: "Product", Position: "Проектный менеджер", HRFormat: "remote",
			Timezone: "Europe/Kaliningrad", DaysShift: 45, WorkFormat: "remote",
			HoursStart: "09:00", HoursEnd: "18:00"},
		{Email: "lena@worktime.local", FullName: "Даниил Постников", Role: "hr",
			Department: "People", Position: "HR-менеджер", HRFormat: "hybrid",
			Timezone: "Europe/Kaliningrad", DaysShift: 50, WorkFormat: "hybrid",
			HoursStart: "09:30", HoursEnd: "18:30"},
		{Email: "sergey@worktime.local", FullName: "Олег Пламадил", Role: "employee",
			Department: "Distributed", Position: "DevOps-инженер", HRFormat: "remote",
			Timezone: "Asia/Novosibirsk", DaysShift: 142, WorkFormat: "remote",
			HoursStart: "09:00", HoursEnd: "18:00",
			Exceptions: []demoException{{Kind: "sick_leave", StartIn: -3, EndIn: 0, Comment: "ОРВИ"}}},
		{Email: "olga@worktime.local", FullName: "Александр Петров", Role: "analyst",
			Department: "Distributed", Position: "Аналитик данных", HRFormat: "remote",
			Timezone: "Asia/Novosibirsk", DaysShift: 95, WorkFormat: "remote",
			HoursStart: "10:00", HoursEnd: "19:00"},
		{Email: "dmitry@worktime.local", FullName: "Даниил Игаев", Role: "employee",
			Department: "Distributed", Position: "Frontend-инженер", HRFormat: "office",
			Timezone: "Europe/Lisbon", DaysShift: 180, WorkFormat: "remote",
			HoursStart: "09:00", HoursEnd: "18:00",
			Exceptions: []demoException{{Kind: "business_trip", StartIn: 21, EndIn: 25, Comment: "Конференция"}}},
		{Email: "tanya@worktime.local", FullName: "Софья Ермолина", Role: "employee",
			Department: "Product", Position: "Дизайнер", HRFormat: "hybrid",
			Timezone: "Europe/Lisbon", DaysShift: 20, WorkFormat: "hybrid",
			HoursStart: "10:00", HoursEnd: "19:00"},
	}
}

func ensureDemoEmployees(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) error {
	people := demoPeople()
	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}

	created := 0
	for _, p := range people {
		var exists bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`,
			p.Email).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := createDemoPerson(ctx, db, p, hash); err != nil {
			log.Warn().Err(err).Str("email", p.Email).Msg("bootstrap: failed to create demo person")
			continue
		}
		created++
	}
	if created > 0 {
		log.Info().Int("count", created).Str("password", demoPassword).
			Msg("bootstrap: demo users created (login password is the same for all)")
	}
	return nil
}

func createDemoPerson(ctx context.Context, db *pgxpool.Pool, p demoPerson, hash string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, full_name, timezone)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, p.Email, hash, p.Role, p.FullName, p.Timezone).Scan(&userID); err != nil {
		return err
	}

	lastUpdate := time.Now().AddDate(0, 0, -p.DaysShift)
	var empID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO employees
			(user_id, department, position, hr_work_format, hire_date,
			 last_profile_update_at, last_confirmed_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4::work_format,
		        $5, $6, $6)
		RETURNING id
	`, userID, p.Department, p.Position, p.HRFormat,
		time.Now().AddDate(-2, 0, 0), lastUpdate).Scan(&empID); err != nil {
		return err
	}

	days := map[string]any{
		"mon": map[string]string{"start": p.HoursStart, "end": p.HoursEnd},
		"tue": map[string]string{"start": p.HoursStart, "end": p.HoursEnd},
		"wed": map[string]string{"start": p.HoursStart, "end": p.HoursEnd},
		"thu": map[string]string{"start": p.HoursStart, "end": p.HoursEnd},
		"fri": map[string]string{"start": p.HoursStart, "end": p.HoursEnd},
	}
	daysJSON, _ := json.Marshal(days)
	if _, err := tx.Exec(ctx, `
		INSERT INTO work_profiles
			(employee_id, valid_from, days_of_week, timezone, work_format, source)
		VALUES ($1, $2, $3::jsonb, $4, $5::work_format, 'manual')
	`, empID, lastUpdate, string(daysJSON), p.Timezone, p.WorkFormat); err != nil {
		return err
	}

	for _, e := range p.Exceptions {
		startAt := time.Now().AddDate(0, 0, e.StartIn)
		endAt := time.Now().AddDate(0, 0, e.EndIn)
		if !endAt.After(startAt) {
			endAt = startAt.Add(24 * time.Hour)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO time_exceptions (employee_id, kind, start_at, end_at, comment, source)
			VALUES ($1, $2::exception_kind, $3, $4, NULLIF($5, ''), 'manual')
		`, empID, e.Kind, startAt, endAt, e.Comment); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// --- демо-команды ---

type demoTeam struct {
	Name         string
	OwnerEmail   string
	MemberEmails []string
}

func demoTeams() []demoTeam {
	return []demoTeam{
		{Name: "Platform", OwnerEmail: "igor@worktime.local",
			MemberEmails: []string{"anna@worktime.local", "igor@worktime.local", "sergey@worktime.local"}},
		{Name: "Product", OwnerEmail: "maria@worktime.local",
			MemberEmails: []string{"maria@worktime.local", "tanya@worktime.local", "lena@worktime.local"}},
		{Name: "Distributed", OwnerEmail: "olga@worktime.local",
			MemberEmails: []string{"sergey@worktime.local", "olga@worktime.local", "dmitry@worktime.local"}},
	}
}

func ensureDemoTeams(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) error {
	for _, t := range demoTeams() {
		var teamID uuid.UUID
		err := db.QueryRow(ctx, `SELECT id FROM teams WHERE name = $1`, t.Name).Scan(&teamID)
		if err != nil {
			ownerID := lookupEmployeeID(ctx, db, t.OwnerEmail)
			if err := db.QueryRow(ctx, `
				INSERT INTO teams (name, owner_id) VALUES ($1, $2) RETURNING id
			`, t.Name, ownerID).Scan(&teamID); err != nil {
				continue
			}
		}
		for _, email := range t.MemberEmails {
			empID := lookupEmployeeID(ctx, db, email)
			if empID == nil {
				continue
			}
			_, _ = db.Exec(ctx, `
				INSERT INTO team_members (team_id, employee_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, teamID, *empID)
		}
	}
	return nil
}

func lookupEmployeeID(ctx context.Context, db *pgxpool.Pool, email string) *uuid.UUID {
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		SELECT e.id FROM employees e JOIN users u ON u.id = e.user_id WHERE u.email = $1
	`, email).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}
