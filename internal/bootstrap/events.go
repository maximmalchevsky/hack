package bootstrap

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ensureDemoEvents — если у демо-сотрудника нет событий, генерит ему пачку.
// Идемпотентно: проверяет по employee_id.
//
// Цель — наполнить систему реалистичными метриками:
// - Игорь (manager/office) — перегруз L>0.8
// - Сергей (NSK/remote) — TZ-drift: встречи в MSK-время выходят за его 9-18 NSK
// - Дмитрий (LIS/remote, HR=office) — HR-mismatch
// - Все — конфликты по выходным/ночью для разнообразия
// ensureDemoEvents возвращает true, если в этом запуске реально были созданы
// события (а не просто всё уже существовало). Это позволяет caller'у решить,
// нужно ли заводить Asynq-задачи на пересчёт.
func ensureDemoEvents(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) (bool, error) {
	people := demoPeople()
	created := 0
	for _, p := range people {
		empID := lookupEmployeeID(ctx, db, p.Email)
		if empID == nil {
			continue
		}

		var count int
		if err := db.QueryRow(ctx, `
			SELECT COUNT(*) FROM calendar_events WHERE employee_id = $1
		`, *empID).Scan(&count); err != nil {
			return false, err
		}
		if count > 0 {
			continue
		}

		events := generateEventsFor(p, *empID)
		for _, ev := range events {
			if _, err := db.Exec(ctx, `
				INSERT INTO calendar_events
					(employee_id, source_event_id, title, start_at, end_at,
					 timezone, attendees_count, organizer, status, raw)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'confirmed', '{"source":"seed"}'::jsonb)
				ON CONFLICT DO NOTHING
			`, *empID, ev.SourceID, ev.Title, ev.StartAt, ev.EndAt,
				ev.Timezone, ev.Attendees, ev.Organizer); err != nil {
				log.Warn().Err(err).Str("email", p.Email).Msg("bootstrap: insert event failed")
			}
		}
		created += len(events)
	}
	if created > 0 {
		log.Info().Int("count", created).Msg("bootstrap: demo calendar events created")
	}
	return created > 0, nil
}

type demoEvent struct {
	SourceID  string
	Title     string
	StartAt   time.Time
	EndAt     time.Time
	Timezone  string
	Attendees int
	Organizer string
}

// generateEventsFor возвращает 15-30 событий на ближайшие [-7, +14] дней
// по сценарию, выбираемому по email сотрудника.
func generateEventsFor(p demoPerson, empID uuid.UUID) []demoEvent {
	// Стабильный seed для воспроизводимости: hash employeeID.
	r := rand.New(rand.NewSource(int64(empID[0])<<24 | int64(empID[1])<<16 | int64(empID[2])<<8 | int64(empID[3])))

	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}

	scenario := pickScenario(p.Email)
	out := make([]demoEvent, 0, 25)

	// Стартуем с понедельника последней недели, генерируем 3 недели вперёд.
	now := time.Now().In(loc)
	monday := startOfWeek(now).AddDate(0, 0, -7)

	for dayOffset := 0; dayOffset < 21; dayOffset++ {
		day := monday.AddDate(0, 0, dayOffset)
		weekday := day.Weekday()

		switch scenario {
		case "overloaded":
			// Игорь: 4-6 встреч в день, плотный график 9-18
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
			out = appendDay(out, r, day, loc, 4+r.Intn(3), 9, 18,
				[]string{"Планёрка", "1-on-1", "Sync с командой", "Архитектурный", "Демо", "Ретро"})

		case "tzdrift":
			// Сергей: NSK (UTC+7), but встречи запланированы в MSK-время (UTC+3)
			// → start_at в UTC, а в его TZ это поздно (15:00 MSK = 19:00 NSK)
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
			// 2-3 события в "его рабочее" (10-12 NSK) + 2-3 события в "MSK-окно" (13-16 MSK = 17-20 NSK)
			out = appendDay(out, r, day, loc, 2, 10, 12,
				[]string{"Stand-up", "Локальная встреча"})
			// MSK-командные — снаружи рабочего профиля сотрудника
			out = appendDay(out, r, day, loc, 2+r.Intn(2), 17, 20,
				[]string{"MSK Sync", "Demo с продактом", "Архитектурный комитет"})

		case "hr_mismatch":
			// Дмитрий: HR говорит "office", фактически remote (LIS UTC+1)
			// → встречи в его утро/вечер, нерегулярный паттерн
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
			out = appendDay(out, r, day, loc, 2+r.Intn(2), 11, 18,
				[]string{"Code review", "1-on-1", "Frontend sync", "Дизайн-ревью"})
			// Иногда встречи поздно вечером (после 19) — типично для remote-кочующих
			if r.Float64() < 0.4 {
				out = appendDay(out, r, day, loc, 1, 19, 21,
					[]string{"Late call"})
			}

		case "weekend_burn":
			// у кого-нибудь должны быть события в выходные — для C>0
			if weekday >= time.Monday && weekday <= time.Friday {
				out = appendDay(out, r, day, loc, 2+r.Intn(2), 10, 18,
					[]string{"Daily", "Sync", "Review"})
			}
			if weekday == time.Saturday && r.Float64() < 0.5 {
				out = appendDay(out, r, day, loc, 1, 11, 13,
					[]string{"Хакатон-Q", "Запасной деплой"})
			}

		case "sparse":
			// Ольга: мало встреч, аналитик
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
			if r.Float64() < 0.6 {
				out = appendDay(out, r, day, loc, 1+r.Intn(2), 10, 16,
					[]string{"Data review", "Метрики недели", "Отчёт"})
			}

		default: // "healthy"
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
			out = appendDay(out, r, day, loc, 2+r.Intn(2), 10, 17,
				[]string{"Stand-up", "1-on-1", "Sync", "Демо", "Ретро"})
		}
	}

	return out
}

func pickScenario(email string) string {
	switch email {
	case "igor@worktime.local": // Игорь Климов
		return "overloaded"
	case "plamadil@worktime.local": // Олег Пламадил (NSK, TZ-drift)
		return "tzdrift"
	case "daniil@iqj.app": // Даниил Игаев (LIS, HR-mismatch)
		return "hr_mismatch"
	case "petrov@worktime.local": // Александр Петров (аналитик)
		return "sparse"
	default:
		return "healthy"
	}
}

func startOfWeek(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // воскресенье → 7
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -(wd - 1))
}

// appendDay генерит count событий случайной длительности 30-90 мин на день day
// в окне [hourFrom, hourTo) с произвольными названиями из titles.
func appendDay(out []demoEvent, r *rand.Rand, day time.Time, loc *time.Location,
	count, hourFrom, hourTo int, titles []string) []demoEvent {
	// слоты по 30 минут, чтобы события не пересекались
	slots := (hourTo-hourFrom)*2 - 1
	if slots <= 0 || count <= 0 {
		return out
	}
	used := map[int]bool{}
	for i := 0; i < count && len(used) < slots; i++ {
		slot := r.Intn(slots)
		for used[slot] {
			slot = r.Intn(slots)
		}
		used[slot] = true
		startHour := hourFrom + slot/2
		startMin := (slot % 2) * 30
		start := time.Date(day.Year(), day.Month(), day.Day(),
			startHour, startMin, 0, 0, loc)
		duration := time.Duration(30+r.Intn(3)*30) * time.Minute // 30/60/90
		end := start.Add(duration)
		title := titles[r.Intn(len(titles))]
		out = append(out, demoEvent{
			SourceID:  fmt.Sprintf("seed-%s-%d-%d", strings.ReplaceAll(title, " ", "_"), day.Unix(), slot),
			Title:     title,
			StartAt:   start.UTC(),
			EndAt:     end.UTC(),
			Timezone:  loc.String(),
			Attendees: 2 + r.Intn(6),
			Organizer: "auto@seed.local",
		})
	}
	return out
}
