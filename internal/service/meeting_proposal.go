package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/integrations"
	"worktimesync/internal/integrations/imip"
	"worktimesync/internal/integrations/yandex"
	"worktimesync/internal/notify"
	"worktimesync/internal/repository"
	"worktimesync/pkg/crypto"
)

// MeetingProposalService — предложение встречи команде:
// шлёт уведомление каждому участнику + инициатору, и (если у инициатора подключён
// Yandex Календарь) — кладёт событие в его календарь через CalDAV PUT.
// ReplanEnqueuer — лёгкий интерфейс для запуска tasks:replan:one из этого
// сервиса без circular dependency. Реализуется *workers.Enqueuer.
type ReplanEnqueuer interface {
	EnqueueTasksReplanOne(employeeID uuid.UUID) error
}

type MeetingProposalService struct {
	pool          *pgxpool.Pool
	teams         *repository.TeamRepo
	users         *repository.UserRepo
	emps          *repository.EmployeeRepo
	integrations  *repository.IntegrationRepo
	events        *repository.CalendarEventRepo
	profiles      *repository.WorkProfileRepo
	notifications *NotificationService
	cipher        *crypto.Cipher
	yandex        *yandex.Provider          // nil если OAuth Яндекса не настроен
	email         *notify.EmailTransport    // nil если SMTP не настроен
	imipReplyTo   string                    // тех. ящик для iMIP. Пусто = инвайты не шлём.
	imipEnabled   bool
	replanEnq     ReplanEnqueuer            // nil — replan не дёргаем
}

func NewMeetingProposalService(pool *pgxpool.Pool, notif *NotificationService) *MeetingProposalService {
	return &MeetingProposalService{
		pool:          pool,
		teams:         repository.NewTeamRepo(pool),
		users:         repository.NewUserRepo(pool),
		emps:          repository.NewEmployeeRepo(pool),
		integrations:  repository.NewIntegrationRepo(pool),
		events:        repository.NewCalendarEventRepo(pool),
		profiles:      repository.NewWorkProfileRepo(pool),
		notifications: notif,
	}
}

// WithYandex — DI: подключаем провайдер Яндекса (для записи событий).
func (s *MeetingProposalService) WithYandex(p *yandex.Provider, cipher *crypto.Cipher) *MeetingProposalService {
	s.yandex = p
	s.cipher = cipher
	return s
}

// WithIMIP — DI: подключаем SMTP-транспорт для рассылки .ics-инвайтов.
// Если enabled=false или replyTo пустой — инвайты не шлём.
func (s *MeetingProposalService) WithIMIP(email *notify.EmailTransport, replyTo string, enabled bool) *MeetingProposalService {
	s.email = email
	s.imipReplyTo = strings.TrimSpace(replyTo)
	s.imipEnabled = enabled && s.imipReplyTo != "" && email != nil && email.Enabled()
	return s
}

// WithReplanEnqueuer — DI: после Propose/Update/Cancel будем дёргать
// tasks:replan:one, чтобы задачи в плане пересчитались с учётом новой встречи.
func (s *MeetingProposalService) WithReplanEnqueuer(enq ReplanEnqueuer) *MeetingProposalService {
	s.replanEnq = enq
	return s
}

// triggerReplan — ставит tasks:replan:one для каждого emp_id из списка.
// Дедуплицирует, фильтрует uuid.Nil. Best-effort — ошибки enqueue игнорим.
func (s *MeetingProposalService) triggerReplan(empIDs ...uuid.UUID) {
	if s.replanEnq == nil {
		return
	}
	seen := map[uuid.UUID]struct{}{}
	for _, id := range empIDs {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		_ = s.replanEnq.EnqueueTasksReplanOne(id)
	}
}

// sendIMIPInvites — отправляет .ics-инвайт каждому участнику с email'ом.
// Best-effort: ошибки логируются, но не валят основной flow Propose().
//
// Не отправляет инициатору — он и так увидит встречу в «Мои встречи» в UI.
// Если хочется чтобы инициатор тоже получил инвайт у себя в Gmail — добавим
// его в attendees отдельно.
func (s *MeetingProposalService) sendIMIPInvites(
	ctx context.Context,
	meetingID uuid.UUID,
	title, description string,
	startAt, endAt time.Time,
	initiatorEmp uuid.UUID,
	initiatorName string,
	teamMembers []domain.TeamMemberDetailed,
) {
	if !s.imipEnabled || s.email == nil {
		return
	}

	// Собираем emails+ФИО участников (кроме инициатора). Один запрос вместо N.
	type recipient struct {
		empID    uuid.UUID
		userID   uuid.UUID
		email    string
		fullName string
	}
	var memberEmpIDs []uuid.UUID
	for _, m := range teamMembers {
		if m.EmployeeID == initiatorEmp {
			continue
		}
		memberEmpIDs = append(memberEmpIDs, m.EmployeeID)
	}
	if len(memberEmpIDs) == 0 {
		return
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.id, COALESCE(u.email, ''), COALESCE(u.full_name, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1) AND u.email <> ''
	`, memberEmpIDs)
	if err != nil {
		return
	}
	defer rows.Close()

	var recipients []recipient
	for rows.Next() {
		var r recipient
		if err := rows.Scan(&r.empID, &r.userID, &r.email, &r.fullName); err != nil {
			continue
		}
		recipients = append(recipients, r)
	}
	if len(recipients) == 0 {
		return
	}

	// Готовим Attendees для .ics — каждый получатель попадает сюда, чтобы
	// клиент календаря показал всех участников как «invited».
	attendees := make([]imip.Attendee, 0, len(recipients))
	for _, r := range recipients {
		attendees = append(attendees, imip.Attendee{Email: r.email, Name: r.fullName})
	}

	icsBody := imip.BuildInvitation(imip.Invitation{
		MeetingID:      meetingID,
		Title:          title,
		Description:    description,
		StartAt:        startAt,
		EndAt:          endAt,
		OrganizerEmail: s.imipReplyTo,
		OrganizerName:  initiatorName,
		Attendees:      attendees,
		Sequence:       0,
		Method:         "REQUEST",
	})

	// Plain-текст для тех клиентов, что не понимают .ics.
	plain := fmt.Sprintf(
		"%s\n\nВремя: %s — %s UTC\nИнициатор: %s\n\nЭто календарный инвайт. Нажми «Принять» в своём клиенте почты, "+
			"и событие добавится в твой календарь.",
		title,
		startAt.Format("02.01.2006 15:04"),
		endAt.Format("15:04"),
		initiatorName,
	)

	subj := title
	for _, r := range recipients {
		if err := s.email.SendCalendarInvite(ctx, r.email, subj, plain, icsBody, s.imipReplyTo, initiatorName); err != nil {
			// Лог через notifications-канал не делаем — best-effort, движемся дальше.
			continue
		}
	}
}

type ProposeMeetingInput struct {
	TeamID        uuid.UUID
	StartAt       time.Time
	EndAt         time.Time
	Title         string
	InitiatorUser uuid.UUID // user_id того, кто запустил предложение
	InitiatorEmp  uuid.UUID // employee_id (если есть)
	// Category — опционально, выбирает пользователь в форме создания встречи.
	// Пустая = «определить автоматически» (GigaChat при подсчёте «куда уходит время»).
	Category string
	// InviteeEmpIDs — явный список приглашённых для межкомандных встреч.
	// Если задан — используем его вместо team.Members. TeamID при этом может
	// быть Nil (тогда встреча не привязана к команде) либо ссылаться на любую
	// «основную» команду для отображения.
	InviteeEmpIDs []uuid.UUID
	// Force — обходит soft-проверку анти-burnout (часов встреч в неделе > порога).
	// Используется когда инициатор сознательно подтвердил «всё равно создать»
	// в UI после предупреждения о перегрузе.
	Force bool
}

type ProposeMeetingResult struct {
	MeetingID      uuid.UUID `json:"meeting_id"`                 // id записи в meeting_proposals
	Sent           int       `json:"sent"`
	TeamName       string    `json:"team_name"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	YandexEventUID string    `json:"yandex_event_uid,omitempty"` // UID события у инициатора (для обратной совместимости)
	YandexPushed   int       `json:"yandex_pushed"`              // сколько Яндекс-календарей всего получили событие (включая инициатора)
}

var (
	ErrMeetingInvalidRange    = errors.New("meeting: end_at must be after start_at")
	ErrMeetingNoParticipants  = errors.New("meeting: no participants — set team_id or invitee_emp_ids")
	// ErrMeetingOverload — soft-block анти-burnout: у одного или нескольких
	// участников после новой встречи превышается WeeklyMeetingHoursLimit.
	// Инициатор может пересоздать с Force=true.
	ErrMeetingOverload        = errors.New("meeting: would overload one or more participants")
)

// OverloadDetails — конкретика для UI: какие сотрудники, при каких числах.
// Возвращается обёрнутым в errMeetingOverload через errors.As.
type OverloadDetails struct {
	Entries []OverloadEntry
}

// Error — реализация error для OverloadDetails, чтобы можно было её через
// fmt.Errorf("%w", details) обернуть с базовой ErrMeetingOverload.
func (o *OverloadDetails) Error() string {
	return ErrMeetingOverload.Error()
}

// Unwrap — связывает с базовой ErrMeetingOverload для errors.Is.
func (o *OverloadDetails) Unwrap() error { return ErrMeetingOverload }

// Propose — для каждого участника команды/списка приглашённых + инициатора
// пушит уведомление. Поддерживает два режима:
//
//   - Командный: задан TeamID, InviteeEmpIDs пустой. Берём всех members
//     команды (текущее поведение).
//   - Межкомандный: задан InviteeEmpIDs. TeamID опционален — если задан,
//     используется только как «основная» команда для отображения.
//
// Уже состоящий в списке инициатор не получает дубликат.
func (s *MeetingProposalService) Propose(ctx context.Context, in ProposeMeetingInput) (*ProposeMeetingResult, error) {
	if !in.EndAt.After(in.StartAt) {
		return nil, ErrMeetingInvalidRange
	}
	if in.TeamID == uuid.Nil && len(in.InviteeEmpIDs) == 0 {
		return nil, ErrMeetingNoParticipants
	}

	// Определяем имя «команды» для display и список members.
	var (
		teamName string
		members  []domain.TeamMemberDetailed
		err      error
	)
	if in.TeamID != uuid.Nil {
		t, terr := s.teams.ByID(ctx, in.TeamID)
		if terr != nil {
			return nil, fmt.Errorf("team: %w", terr)
		}
		teamName = t.Name
	}

	if len(in.InviteeEmpIDs) > 0 {
		// Межкомандный режим — берём явный список emp_id.
		members, err = s.expandInvitees(ctx, in.InviteeEmpIDs)
		if err != nil {
			return nil, fmt.Errorf("expand invitees: %w", err)
		}
		if teamName == "" {
			teamName = "Несколько команд"
		}
	} else {
		// Командный — старый путь.
		members, err = s.teams.Members(ctx, in.TeamID)
		if err != nil {
			return nil, err
		}
	}

	// Анти-burnout soft-block: проверяем что у участников после новой встречи
	// не превысится WeeklyMeetingHoursLimit. Если превышается и инициатор НЕ
	// прислал Force=true — возвращаем ErrMeetingOverload c деталями.
	if !in.Force {
		empIDsForCheck := make([]uuid.UUID, 0, len(members)+1)
		if in.InitiatorEmp != uuid.Nil {
			empIDsForCheck = append(empIDsForCheck, in.InitiatorEmp)
		}
		for _, m := range members {
			if m.EmployeeID != in.InitiatorEmp {
				empIDsForCheck = append(empIDsForCheck, m.EmployeeID)
			}
		}
		if overload, oerr := s.CheckOverload(ctx, in.StartAt, in.EndAt, empIDsForCheck); oerr == nil && len(overload) > 0 {
			return nil, &OverloadDetails{Entries: overload}
		}
	}

	// Имя инициатора — для красивого body.
	initiatorName := ""
	if u, err := s.users.ByID(ctx, in.InitiatorUser); err == nil && u != nil {
		initiatorName = u.FullName
	}

	title := in.Title
	if title == "" {
		title = "Встреча команды «" + teamName + "»"
	}

	body := formatMeetingBody(in.StartAt, in.EndAt, initiatorName)

	// Резолвим user_id всех участников за один запрос.
	memberEmpIDs := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		memberEmpIDs = append(memberEmpIDs, m.EmployeeID)
	}
	userIDs, err := s.fetchUserIDs(ctx, memberEmpIDs)
	if err != nil {
		return nil, err
	}

	// Включаем инициатора, даже если он не в команде (например, HR пушит чужой команде).
	seen := map[uuid.UUID]struct{}{}
	if in.InitiatorUser != uuid.Nil {
		userIDs = append(userIDs, in.InitiatorUser)
	}

	sent := 0
	for _, uid := range userIDs {
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}

		_, err := s.notifications.Push(ctx, CreateInput{
			UserID: uid,
			Kind:   "meeting_proposal",
			Title:  title,
			Body:   body,
			Link:   "/team-map",
			Payload: map[string]any{
				"team_id":      nullableUUID(in.TeamID),
				"team_name":    teamName,
				"start_at":     in.StartAt,
				"end_at":       in.EndAt,
				"initiator_id": in.InitiatorUser.String(),
			},
		})
		if err != nil {
			continue
		}
		sent++
	}

	// Категорию валидируем — если пользователь прислал не из списка, обнуляем
	// (пусть AI решит), чтобы не плодить мусорные значения.
	category := validateProposalCategory(in.Category)

	// Сохраняем proposal до пуша в Yandex — чтобы было куда привязывать pushes.
	var meetingID uuid.UUID
	insErr := s.pool.QueryRow(ctx, `
		INSERT INTO meeting_proposals (
			initiator_user, initiator_emp, team_id, title, start_at, end_at, category
		) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
		RETURNING id
	`, nullableUUID(in.InitiatorUser), nullableUUID(in.InitiatorEmp), nullableUUID(in.TeamID),
		title, in.StartAt, in.EndAt, category,
	).Scan(&meetingID)
	if insErr != nil {
		return nil, fmt.Errorf("save proposal: %w", insErr)
	}

	res := &ProposeMeetingResult{
		MeetingID: meetingID,
		Sent:      sent,
		TeamName:  teamName,
		StartAt:   in.StartAt,
		EndAt:     in.EndAt,
	}

	// Создаём строки meeting_responses:
	//   - инициатор — сразу 'accepted' (он же создал)
	//   - остальные участники команды — 'pending' (ждём accept)
	allEmpIDs := []uuid.UUID{}
	if in.InitiatorEmp != uuid.Nil {
		allEmpIDs = append(allEmpIDs, in.InitiatorEmp)
	}
	for _, m := range members {
		if m.EmployeeID == in.InitiatorEmp {
			continue
		}
		allEmpIDs = append(allEmpIDs, m.EmployeeID)
	}
	for _, empID := range allEmpIDs {
		status := "pending"
		var respondedAt any = nil
		if empID == in.InitiatorEmp {
			status = "accepted"
			respondedAt = time.Now()
		}
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO meeting_responses (meeting_id, employee_id, status, responded_at)
			VALUES ($1, $2, $3::meeting_response_status, $4)
			ON CONFLICT (meeting_id, employee_id) DO NOTHING
		`, meetingID, empID, status, respondedAt)
	}

	// Yandex push — ТОЛЬКО инициатору сразу. Остальные участники получат
	// событие в свой Яндекс при accept (опционально, по чекбоксу).
	yandexCreated := false
	if in.InitiatorEmp != uuid.Nil {
		pushed := s.pushYandexForEmployee(ctx, meetingID, in.InitiatorEmp, in, teamName, initiatorName, category)
		if pushed != nil {
			yandexCreated = true
			res.YandexPushed++
			res.YandexEventUID = pushed.UID
			// Помечаем что в Яндекс инициатора положили.
			_, _ = s.pool.Exec(ctx, `
				UPDATE meeting_responses SET yandex_pushed = true
				WHERE meeting_id = $1 AND employee_id = $2
			`, meetingID, in.InitiatorEmp)
		}
	}

	// Если Yandex-push не сработал (нет интеграции или ошибка) — всё равно
	// создаём calendar_event у инициатора напрямую. Без этого встреча
	// не показывается в /dashboard, /workload, find-window — никуда.
	if !yandexCreated && in.InitiatorEmp != uuid.Nil && s.events != nil {
		src := "meeting-" + meetingID.String() + "-" + in.InitiatorEmp.String()
		_, _ = s.events.Upsert(ctx, repository.UpsertEventInput{
			EmployeeID:    in.InitiatorEmp,
			IntegrationID: nil,
			SourceEventID: src,
			Title:         title,
			Description:   fmt.Sprintf("Команда: %s. Инициатор: %s.", teamName, initiatorName),
			StartAt:       in.StartAt,
			EndAt:         in.EndAt,
			Status:        domain.EventConfirmed,
			Category:      category,
		})
	}

	// iMIP-инвайт всем участникам с email'ом. Best-effort: если SMTP/IMIP
	// не настроены — функция тихо выходит. Это идёт ОТДЕЛЬНО от notifications.Push,
	// которая шлёт обычное текстовое уведомление: получатель видит два письма —
	// текстовое и календарный инвайт с кнопкой Accept.
	s.sendIMIPInvites(ctx, meetingID, title,
		fmt.Sprintf("Команда: %s. Инициатор: %s.", teamName, initiatorName),
		in.StartAt, in.EndAt,
		in.InitiatorEmp, initiatorName, members,
	)

	// Пересчёт плана задач затронутых сотрудников — чтобы task-блоки
	// обтекали новую встречу. Инициатор + accept'нутые участники.
	// Остальные пересчитают по cron'у когда accept'нут.
	replanIDs := []uuid.UUID{in.InitiatorEmp}
	for _, m := range members {
		replanIDs = append(replanIDs, m.EmployeeID)
	}
	s.triggerReplan(replanIDs...)

	return res, nil
}

// nullableUUID — helper: возвращает интерфейсный nil для uuid.Nil, чтобы в
// колонки с NULL writeable не падал FK на несуществующий 00000000-... uuid.
func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// expandInvitees — превращает явный список emp_id в []TeamMemberDetailed.
// Один SQL запрос, дедупликация, защита от uuid.Nil. Если какой-то emp не найден
// (удалён, например) — просто пропускаем, не валим всю операцию.
func (s *MeetingProposalService) expandInvitees(ctx context.Context, ids []uuid.UUID) ([]domain.TeamMemberDetailed, error) {
	uniq := make(map[uuid.UUID]struct{}, len(ids))
	cleaned := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(u.full_name, ''),
		       COALESCE(u.role::text, ''),
		       COALESCE(e.department, ''),
		       COALESCE(u.timezone, ''),
		       e.hr_work_format
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, cleaned)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.TeamMemberDetailed, 0, len(cleaned))
	for rows.Next() {
		var m domain.TeamMemberDetailed
		if err := rows.Scan(&m.EmployeeID, &m.FullName, &m.Role,
			&m.Department, &m.Timezone, &m.WorkFormat); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// validateProposalCategory — если пользователь прислал не из канонического
// списка → возвращает пустую строку. Иначе — приводит к каноничному виду.
// Пустая строка означает «определить автоматически».
func validateProposalCategory(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, c := range TimeBreakdownCategories {
		if strings.EqualFold(s, c) {
			return c
		}
	}
	return ""
}

// pushYandexForEmployee — для одного сотрудника: ищет активную Yandex-интеграцию,
// дешифрует токен, рефрешит если надо, делает CalDAV PUT и сохраняет push
// в БД для последующей отмены. Если передана `category` — пишет её в
// calendar_events (Upsert), чтобы дальше не пере-классифицировать.
// Возвращает результат CreateEvent или nil.
func (s *MeetingProposalService) pushYandexForEmployee(
	ctx context.Context,
	meetingID, empID uuid.UUID,
	in ProposeMeetingInput,
	teamName, initiatorName, category string,
) *yandex.CreateEventResult {
	if s.yandex == nil || s.cipher == nil || empID == uuid.Nil {
		return nil
	}

	integ := s.findYandexIntegration(ctx, empID)
	if integ == nil {
		return nil
	}

	access, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil || access == "" {
		return nil
	}
	refresh := ""
	if integ.RefreshTokenEnc != "" {
		if r, derr := s.cipher.Decrypt(integ.RefreshTokenEnc); derr == nil {
			refresh = r
		}
	}

	tok := &integrations.Token{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "OAuth",
		Raw:          map[string]any{},
	}
	if integ.ExpiresAt != nil {
		tok.Expiry = *integ.ExpiresAt
	}

	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < time.Minute && refresh != "" {
		if newTok, rerr := s.yandex.RefreshToken(ctx, tok); rerr == nil && newTok != nil {
			tok = newTok
			if enc, eerr := s.cipher.Encrypt(tok.AccessToken); eerr == nil {
				refEnc := ""
				if tok.RefreshToken != "" {
					if r, err := s.cipher.Encrypt(tok.RefreshToken); err == nil {
						refEnc = r
					}
				}
				_ = s.integrations.UpdateTokens(ctx, integ.ID, enc, refEnc, tok.Expiry)
			}
		}
	}

	title := in.Title
	if title == "" {
		title = "Встреча команды «" + teamName + "»"
	}

	created, err := s.yandex.CreateEvent(ctx, tok, yandex.CreateEventInput{
		Title:       title,
		Description: fmt.Sprintf("Команда: %s. Инициатор: %s.", teamName, initiatorName),
		StartAt:     in.StartAt,
		EndAt:       in.EndAt,
		Organizer:   integ.AccountEmail,
	})
	if err != nil || created == nil {
		return nil
	}

	// Запоминаем push, чтобы потом уметь сделать DELETE.
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO meeting_pushes (
			meeting_id, employee_id, integration_id, provider,
			source_event_uid, calendar_path
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, meetingID, empID, integ.ID, string(integ.Provider), created.UID, created.CalendarPath)

	// Сразу пишем событие в calendar_events с выбранной категорией —
	// чтобы при следующем sync Upsert не пере-классифицировал её через AI.
	// Если category пустая — поле остаётся NULL и AI разберётся при подсчёте
	// «куда уходит время».
	if s.events != nil {
		integID := integ.ID
		_, _ = s.events.Upsert(ctx, repository.UpsertEventInput{
			EmployeeID:    empID,
			IntegrationID: &integID,
			SourceEventID: created.UID,
			Title:         title,
			StartAt:       in.StartAt,
			EndAt:         in.EndAt,
			Organizer:     integ.AccountEmail,
			Status:        domain.EventConfirmed,
			Category:      category,
		})
	}

	return created
}

func (s *MeetingProposalService) findYandexIntegration(ctx context.Context, empID uuid.UUID) *domain.Integration {
	list, err := s.integrations.ListByEmployee(ctx, empID)
	if err != nil {
		return nil
	}
	for _, i := range list {
		if i.Provider == domain.IntegrationYandexCalendar &&
			i.Status != domain.IntegrationStatusError {
			ic := i
			return &ic
		}
	}
	return nil
}

func (s *MeetingProposalService) fetchUserIDs(ctx context.Context, empIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(empIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT user_id FROM employees WHERE id = ANY($1)
	`, empIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]uuid.UUID, 0, len(empIDs))
	for rows.Next() {
		var uid uuid.UUID
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

func formatMeetingBody(start, end time.Time, initiator string) string {
	day := start.Format("02.01.2006")
	startHM := start.Format("15:04")
	endHM := end.Format("15:04")
	if initiator != "" {
		return fmt.Sprintf("Предложено: %s, %s–%s UTC. Инициатор: %s.", day, startHM, endHM, initiator)
	}
	return fmt.Sprintf("Предложено: %s, %s–%s UTC.", day, startHM, endHM)
}

// --- Рассылка запросов на обновление графика (HR-сценарий) ---

type StaleNotifyResult struct {
	Sent     int      `json:"sent"`
	Skipped  int      `json:"skipped"`
	Targeted int      `json:"targeted"`
	Emails   []string `json:"emails,omitempty"`
}

// NotifyStaleProfiles — берёт всех сотрудников, у которых last_profile_update_at
// старше minDaysSince (или вообще нет), и пушит каждому уведомление
// «Обновите рабочий график». Дедуп: если в последние 24 часа уже было такое
// уведомление — пропускаем.
func (s *MeetingProposalService) NotifyStaleProfiles(
	ctx context.Context,
	minDaysSince int,
	initiatorUser uuid.UUID,
) (*StaleNotifyResult, error) {
	if minDaysSince < 0 {
		minDaysSince = 60
	}

	// 1. Список employees + user_ids с просроченным графиком.
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.email, u.full_name,
		       COALESCE(EXTRACT(DAY FROM now() - e.last_profile_update_at)::int, 9999) AS days
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.last_profile_update_at IS NULL
		   OR e.last_profile_update_at < now() - make_interval(days => $1)
		ORDER BY days DESC
	`, minDaysSince)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		userID   uuid.UUID
		email    string
		fullName string
		days     int
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.userID, &c.email, &c.fullName, &c.days); err != nil {
			continue
		}
		cands = append(cands, c)
	}

	// 2. Дедуп: чьи user_id за последние 24ч уже получали 'request_update'.
	recent := map[uuid.UUID]struct{}{}
	if len(cands) > 0 {
		ids := make([]uuid.UUID, 0, len(cands))
		for _, c := range cands {
			ids = append(ids, c.userID)
		}
		dedupRows, derr := s.pool.Query(ctx, `
			SELECT user_id FROM notifications
			WHERE kind = 'request_update'
			  AND user_id = ANY($1)
			  AND created_at > now() - interval '24 hours'
		`, ids)
		if derr == nil {
			defer dedupRows.Close()
			for dedupRows.Next() {
				var u uuid.UUID
				if scanErr := dedupRows.Scan(&u); scanErr == nil {
					recent[u] = struct{}{}
				}
			}
		}
	}

	res := &StaleNotifyResult{Targeted: len(cands)}
	for _, c := range cands {
		if _, dup := recent[c.userID]; dup {
			res.Skipped++
			continue
		}
		_, err := s.notifications.Push(ctx, CreateInput{
			UserID: c.userID,
			Kind:   "request_update",
			Title:  "Пожалуйста, обновите рабочий график",
			Body: fmt.Sprintf(
				"Профиль не обновлялся %d дней. Зайдите в /profile, отметьте текущие рабочие часы и подтвердите актуальность.",
				c.days,
			),
			Link: "/profile",
			Payload: map[string]any{
				"days_since_update": c.days,
				"initiator_id":      initiatorUser.String(),
			},
		})
		if err != nil {
			continue
		}
		res.Sent++
		res.Emails = append(res.Emails, c.email)
	}
	return res, nil
}

// --- Список и отмена встреч ---

// MyMeeting — одна созданная встреча для UI «Мои встречи» на /scheduler.
type MyMeeting struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	StartAt      time.Time  `json:"start_at"`
	EndAt        time.Time  `json:"end_at"`
	TeamID       *uuid.UUID `json:"team_id,omitempty"`
	TeamName     string     `json:"team_name,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	YandexPushed int        `json:"yandex_pushed"` // активных pushes (не удалённых)
	IsOwner      bool       `json:"is_owner"`      // viewer — инициатор
	CanCancel    bool       `json:"can_cancel"`    // RBAC: инициатор/owner команды/admin
	// Счётчики ответов на это приглашение.
	Accepted     int `json:"accepted"`
	Declined     int `json:"declined"`
	Pending      int `json:"pending"`
	TotalInvited int `json:"total_invited"`
}

var (
	ErrMeetingNotFound        = errors.New("meeting: not found")
	ErrMeetingAlreadyCanceled = errors.New("meeting: already cancelled")
	ErrMeetingForbidden       = errors.New("meeting: forbidden")
	ErrMeetingInvalidUpdate   = errors.New("meeting: invalid update")
)

// UpdateMeetingInput — что разрешено менять. nil = «не менять».
type UpdateMeetingInput struct {
	Title   *string
	StartAt *time.Time
	EndAt   *time.Time
}

// ListMy — список встреч для отображения на /scheduler.
//   - employee: только свои (initiator_emp = viewerEmp).
//   - manager/pm: свои + где команда принадлежит viewer (team.owner_id).
//   - hr/admin: все будущие активные.
//
// Для простоты — показываем только активные (cancelled_at IS NULL) и
// будущие (end_at > now()). История пока не нужна.
func (s *MeetingProposalService) ListMy(
	ctx context.Context,
	viewerUser uuid.UUID,
	viewerEmp uuid.UUID,
	role domain.Role,
) ([]MyMeeting, error) {
	isAdmin := role == domain.RoleAdmin || role == domain.RoleHR
	isManager := role == domain.RoleManager || role == domain.RolePM

	var (
		sql  string
		args []any
	)
	switch {
	case isAdmin:
		sql = `
			SELECT mp.id, mp.title, mp.start_at, mp.end_at,
			       mp.team_id, COALESCE(t.name, ''),
			       mp.created_at, mp.cancelled_at,
			       mp.initiator_user, mp.initiator_emp
			FROM meeting_proposals mp
			LEFT JOIN teams t ON t.id = mp.team_id
			WHERE mp.cancelled_at IS NULL
			  AND mp.end_at > now()
			ORDER BY mp.start_at
		`
	case isManager:
		sql = `
			SELECT mp.id, mp.title, mp.start_at, mp.end_at,
			       mp.team_id, COALESCE(t.name, ''),
			       mp.created_at, mp.cancelled_at,
			       mp.initiator_user, mp.initiator_emp
			FROM meeting_proposals mp
			LEFT JOIN teams t ON t.id = mp.team_id
			WHERE mp.cancelled_at IS NULL
			  AND mp.end_at > now()
			  AND (mp.initiator_emp = $1 OR t.owner_id = $1)
			ORDER BY mp.start_at
		`
		args = []any{viewerEmp}
	default:
		sql = `
			SELECT mp.id, mp.title, mp.start_at, mp.end_at,
			       mp.team_id, COALESCE(t.name, ''),
			       mp.created_at, mp.cancelled_at,
			       mp.initiator_user, mp.initiator_emp
			FROM meeting_proposals mp
			LEFT JOIN teams t ON t.id = mp.team_id
			WHERE mp.cancelled_at IS NULL
			  AND mp.end_at > now()
			  AND mp.initiator_emp = $1
			ORDER BY mp.start_at
		`
		args = []any{viewerEmp}
	}

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MyMeeting{}
	var ids []uuid.UUID
	type rec struct {
		m           MyMeeting
		initUser    *uuid.UUID
		initEmp     *uuid.UUID
		teamOwnerEq bool
	}
	rs := []rec{}
	for rows.Next() {
		var (
			r        rec
			teamID   *uuid.UUID
			cancAt   *time.Time
			teamName string
		)
		if err := rows.Scan(
			&r.m.ID, &r.m.Title, &r.m.StartAt, &r.m.EndAt,
			&teamID, &teamName,
			&r.m.CreatedAt, &cancAt,
			&r.initUser, &r.initEmp,
		); err != nil {
			continue
		}
		r.m.TeamID = teamID
		r.m.TeamName = teamName
		r.m.CancelledAt = cancAt

		// is_owner / can_cancel.
		if r.initUser != nil && *r.initUser == viewerUser {
			r.m.IsOwner = true
		}
		r.m.CanCancel = r.m.IsOwner || isAdmin
		if !r.m.CanCancel && isManager && teamID != nil {
			// owner команды разрешим — проверим ниже одним запросом.
			r.teamOwnerEq = true
		}
		ids = append(ids, r.m.ID)
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Считаем активные pushes (одним запросом для всех).
	pushes := map[uuid.UUID]int{}
	if len(ids) > 0 {
		pr, err := s.pool.Query(ctx, `
			SELECT meeting_id, count(*)
			FROM meeting_pushes
			WHERE meeting_id = ANY($1) AND deleted_at IS NULL
			GROUP BY meeting_id
		`, ids)
		if err == nil {
			defer pr.Close()
			for pr.Next() {
				var id uuid.UUID
				var cnt int
				if err := pr.Scan(&id, &cnt); err == nil {
					pushes[id] = cnt
				}
			}
		}
	}

	// Резолвим can_cancel для manager: проверяем team.owner_id = viewerEmp.
	ownerTeams := map[uuid.UUID]bool{}
	if isManager {
		or, err := s.pool.Query(ctx, `SELECT id FROM teams WHERE owner_id = $1`, viewerEmp)
		if err == nil {
			defer or.Close()
			for or.Next() {
				var id uuid.UUID
				if err := or.Scan(&id); err == nil {
					ownerTeams[id] = true
				}
			}
		}
	}

	// Счётчики ответов по meeting_responses.
	type respStats struct{ accepted, declined, pending int }
	stats := map[uuid.UUID]respStats{}
	if len(ids) > 0 {
		sr, err := s.pool.Query(ctx, `
			SELECT meeting_id, status, count(*)
			FROM meeting_responses
			WHERE meeting_id = ANY($1)
			GROUP BY meeting_id, status
		`, ids)
		if err == nil {
			defer sr.Close()
			for sr.Next() {
				var id uuid.UUID
				var status string
				var cnt int
				if err := sr.Scan(&id, &status, &cnt); err == nil {
					st := stats[id]
					switch status {
					case "accepted":
						st.accepted = cnt
					case "declined":
						st.declined = cnt
					case "pending":
						st.pending = cnt
					}
					stats[id] = st
				}
			}
		}
	}

	for _, r := range rs {
		r.m.YandexPushed = pushes[r.m.ID]
		if !r.m.CanCancel && isManager && r.m.TeamID != nil && ownerTeams[*r.m.TeamID] {
			r.m.CanCancel = true
		}
		s := stats[r.m.ID]
		r.m.Accepted = s.accepted
		r.m.Declined = s.declined
		r.m.Pending = s.pending
		r.m.TotalInvited = s.accepted + s.declined + s.pending
		out = append(out, r.m)
	}
	return out, nil
}

// Cancel — отменяет встречу: шлёт DELETE во все Yandex-календари, куда мы её
// положили, обновляет meeting_proposals.cancelled_at, рассылает уведомления.
//
// Идемпотентность: если встреча уже отменена — возвращаем ErrMeetingAlreadyCanceled.
// DeleteEvent сам толерантен к 404 (событие уже удалили в Яндексе вручную).
func (s *MeetingProposalService) Cancel(
	ctx context.Context,
	meetingID uuid.UUID,
	cancellerUser uuid.UUID,
	cancellerEmp uuid.UUID,
	role domain.Role,
) error {
	// 1. Читаем встречу с FOR UPDATE — чтобы параллельные cancel не дрались.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		initiatorUser *uuid.UUID
		initiatorEmp  *uuid.UUID
		teamID        *uuid.UUID
		title         string
		startAt       time.Time
		endAt         time.Time
		cancelledAt   *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT initiator_user, initiator_emp, team_id, title, start_at, end_at, cancelled_at
		FROM meeting_proposals
		WHERE id = $1
		FOR UPDATE
	`, meetingID).Scan(&initiatorUser, &initiatorEmp, &teamID, &title, &startAt, &endAt, &cancelledAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMeetingNotFound
		}
		return err
	}
	if cancelledAt != nil {
		return ErrMeetingAlreadyCanceled
	}

	// 2. RBAC.
	isAdmin := role == domain.RoleAdmin || role == domain.RoleHR
	isInitiator := initiatorUser != nil && *initiatorUser == cancellerUser
	isManager := role == domain.RoleManager || role == domain.RolePM
	isTeamOwner := false
	if isManager && teamID != nil {
		var ownerID *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT owner_id FROM teams WHERE id = $1`, *teamID).Scan(&ownerID)
		if ownerID != nil && *ownerID == cancellerEmp {
			isTeamOwner = true
		}
	}
	if !isAdmin && !isInitiator && !isTeamOwner {
		return ErrMeetingForbidden
	}

	// 3. Помечаем cancelled в БД.
	if _, err := tx.Exec(ctx, `
		UPDATE meeting_proposals
		SET cancelled_at = now(), cancelled_by = $1
		WHERE id = $2
	`, cancellerUser, meetingID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 3.1. Удаляем native calendar_events которые мы создали в Propose() —
	// иначе после отмены встреча продолжит висеть в /dashboard как busy.
	// source_event_id вида "meeting-<meetingID>-<empID>" — фильтр по prefix.
	_, _ = s.pool.Exec(ctx, `
		DELETE FROM calendar_events
		WHERE integration_id IS NULL
		  AND source_event_id LIKE 'meeting-' || $1::text || '-%'
	`, meetingID)

	// 4. DELETE в Yandex для каждого активного push (best-effort, не падаем на ошибках).
	type pushRow struct {
		id            uuid.UUID
		empID         uuid.UUID
		integrationID *uuid.UUID
		uid           string
		calPath       string
	}
	pushes := []pushRow{}
	pr, err := s.pool.Query(ctx, `
		SELECT id, employee_id, integration_id, source_event_uid, COALESCE(calendar_path, '')
		FROM meeting_pushes
		WHERE meeting_id = $1 AND deleted_at IS NULL
	`, meetingID)
	if err == nil {
		defer pr.Close()
		for pr.Next() {
			var r pushRow
			if err := pr.Scan(&r.id, &r.empID, &r.integrationID, &r.uid, &r.calPath); err == nil {
				pushes = append(pushes, r)
			}
		}
	}
	for _, p := range pushes {
		errMsg := ""
		if delErr := s.deletePush(ctx, p.empID, p.calPath, p.uid); delErr != nil {
			errMsg = delErr.Error()
		}
		if errMsg == "" {
			_, _ = s.pool.Exec(ctx, `
				UPDATE meeting_pushes SET deleted_at = now(), delete_error = NULL
				WHERE id = $1
			`, p.id)
		} else {
			_, _ = s.pool.Exec(ctx, `
				UPDATE meeting_pushes SET delete_error = $1
				WHERE id = $2
			`, errMsg, p.id)
		}
	}

	// 5. Уведомляем участников: cobre notification «Встреча отменена».
	teamName := ""
	if teamID != nil {
		_ = s.pool.QueryRow(ctx, `SELECT name FROM teams WHERE id = $1`, *teamID).Scan(&teamName)
	}
	body := fmt.Sprintf("Встреча %s, %s–%s UTC отменена.",
		startAt.Format("02.01.2006"),
		startAt.Format("15:04"),
		endAt.Format("15:04"),
	)

	// Кому слать: все участники команды + инициатор.
	recipientUserIDs := map[uuid.UUID]struct{}{}
	if initiatorUser != nil {
		recipientUserIDs[*initiatorUser] = struct{}{}
	}
	if teamID != nil {
		members, _ := s.teams.Members(ctx, *teamID)
		empIDs := make([]uuid.UUID, 0, len(members))
		for _, m := range members {
			empIDs = append(empIDs, m.EmployeeID)
		}
		uids, _ := s.fetchUserIDs(ctx, empIDs)
		for _, u := range uids {
			recipientUserIDs[u] = struct{}{}
		}
	}
	for uid := range recipientUserIDs {
		_, _ = s.notifications.Push(ctx, CreateInput{
			UserID: uid,
			Kind:   "meeting_cancelled",
			Title:  title,
			Body:   body,
			Link:   "/scheduler",
			Payload: map[string]any{
				"meeting_id":  meetingID.String(),
				"cancelled_by": cancellerUser.String(),
			},
		})
	}

	// Replan плана задач затронутых: освобождённое время теперь может
	// заполниться задачами.
	replanIDs := []uuid.UUID{}
	if initiatorEmp != nil {
		replanIDs = append(replanIDs, *initiatorEmp)
	}
	if teamID != nil {
		mems, _ := s.teams.Members(ctx, *teamID)
		for _, m := range mems {
			replanIDs = append(replanIDs, m.EmployeeID)
		}
	}
	s.triggerReplan(replanIDs...)

	return nil
}

// Update — изменяет встречу (title/start/end), пушит PUT в Yandex для каждого
// активного push, рассылает уведомление «перенесена».
//
// RBAC такая же как Cancel: инициатор, владелец команды (manager/pm), admin/hr.
func (s *MeetingProposalService) Update(
	ctx context.Context,
	meetingID uuid.UUID,
	editorUser uuid.UUID,
	editorEmp uuid.UUID,
	role domain.Role,
	in UpdateMeetingInput,
) error {
	if in.Title == nil && in.StartAt == nil && in.EndAt == nil {
		return ErrMeetingInvalidUpdate
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		initiatorUser *uuid.UUID
		initiatorEmp  *uuid.UUID
		teamID        *uuid.UUID
		currTitle     string
		currStart     time.Time
		currEnd       time.Time
		cancelledAt   *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT initiator_user, initiator_emp, team_id, title, start_at, end_at, cancelled_at
		FROM meeting_proposals
		WHERE id = $1
		FOR UPDATE
	`, meetingID).Scan(&initiatorUser, &initiatorEmp, &teamID, &currTitle, &currStart, &currEnd, &cancelledAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMeetingNotFound
		}
		return err
	}
	if cancelledAt != nil {
		return ErrMeetingAlreadyCanceled
	}

	// RBAC.
	isAdmin := role == domain.RoleAdmin || role == domain.RoleHR
	isInitiator := initiatorUser != nil && *initiatorUser == editorUser
	isManager := role == domain.RoleManager || role == domain.RolePM
	isTeamOwner := false
	if isManager && teamID != nil {
		var ownerID *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT owner_id FROM teams WHERE id = $1`, *teamID).Scan(&ownerID)
		if ownerID != nil && *ownerID == editorEmp {
			isTeamOwner = true
		}
	}
	if !isAdmin && !isInitiator && !isTeamOwner {
		return ErrMeetingForbidden
	}

	// Применяем правки.
	newTitle := currTitle
	newStart := currStart
	newEnd := currEnd
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t != "" {
			newTitle = t
		}
	}
	if in.StartAt != nil {
		newStart = *in.StartAt
	}
	if in.EndAt != nil {
		newEnd = *in.EndAt
	}
	if !newEnd.After(newStart) {
		return ErrMeetingInvalidRange
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meeting_proposals
		SET title = $1, start_at = $2, end_at = $3
		WHERE id = $4
	`, newTitle, newStart, newEnd, meetingID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Обновляем native calendar_events (созданные в Propose() для инициатора
	// и accept'нувших участников). Source_event_id у них вида
	// 'meeting-<meetingID>-<empID>'.
	_, _ = s.pool.Exec(ctx, `
		UPDATE calendar_events
		SET title = $1, start_at = $2, end_at = $3
		WHERE integration_id IS NULL
		  AND source_event_id LIKE 'meeting-' || $4::text || '-%'
	`, newTitle, newStart, newEnd, meetingID)

	// PUT в Yandex для всех активных pushes (best-effort).
	type pushRow struct {
		id            uuid.UUID
		empID         uuid.UUID
		integrationID *uuid.UUID
		uid           string
		calPath       string
	}
	var pushes []pushRow
	pr, err := s.pool.Query(ctx, `
		SELECT id, employee_id, integration_id, source_event_uid, COALESCE(calendar_path, '')
		FROM meeting_pushes
		WHERE meeting_id = $1 AND deleted_at IS NULL
	`, meetingID)
	if err == nil {
		defer pr.Close()
		for pr.Next() {
			var r pushRow
			if err := pr.Scan(&r.id, &r.empID, &r.integrationID, &r.uid, &r.calPath); err == nil {
				pushes = append(pushes, r)
			}
		}
	}

	teamName := ""
	if teamID != nil {
		_ = s.pool.QueryRow(ctx, `SELECT name FROM teams WHERE id = $1`, *teamID).Scan(&teamName)
	}
	editorName := ""
	if u, err := s.users.ByID(ctx, editorUser); err == nil && u != nil {
		editorName = u.FullName
	}

	for _, p := range pushes {
		_ = s.updatePushYandex(ctx, p.empID, p.calPath, p.uid, yandex.CreateEventInput{
			Title:       newTitle,
			Description: fmt.Sprintf("Команда: %s. Инициатор: %s.", teamName, editorName),
			StartAt:     newStart,
			EndAt:       newEnd,
		})
	}

	// Уведомляем участников: «встреча перенесена».
	body := formatMoveBody(currStart, currEnd, newStart, newEnd)
	recipientUserIDs := map[uuid.UUID]struct{}{}
	if initiatorUser != nil {
		recipientUserIDs[*initiatorUser] = struct{}{}
	}
	if teamID != nil {
		members, _ := s.teams.Members(ctx, *teamID)
		empIDs := make([]uuid.UUID, 0, len(members))
		for _, m := range members {
			empIDs = append(empIDs, m.EmployeeID)
		}
		uids, _ := s.fetchUserIDs(ctx, empIDs)
		for _, u := range uids {
			recipientUserIDs[u] = struct{}{}
		}
	}
	for uid := range recipientUserIDs {
		// Дедуп: предыдущее meeting_updated по этой же встрече — удаляем.
		_, _ = s.pool.Exec(ctx, `
			DELETE FROM notifications
			WHERE user_id = $1
			  AND kind = 'meeting_updated'
			  AND COALESCE(payload->>'meeting_id', '') = $2
		`, uid, meetingID.String())

		_, _ = s.notifications.Push(ctx, CreateInput{
			UserID: uid,
			Kind:   "meeting_updated",
			Title:  newTitle,
			Body:   body,
			Link:   "/scheduler",
			Payload: map[string]any{
				"meeting_id": meetingID.String(),
				"editor":     editorUser.String(),
				"start_at":   newStart,
				"end_at":     newEnd,
			},
		})
	}

	// Replan плана задач затронутых сотрудников: смещение встречи могло
	// освободить старое время или занять новое — task-блоки должны переразложиться.
	replanIDs := []uuid.UUID{}
	if initiatorEmp != nil {
		replanIDs = append(replanIDs, *initiatorEmp)
	}
	if teamID != nil {
		mems, _ := s.teams.Members(ctx, *teamID)
		for _, m := range mems {
			replanIDs = append(replanIDs, m.EmployeeID)
		}
	}
	s.triggerReplan(replanIDs...)

	return nil
}

// formatMoveBody — короткий текст для нотификации «перенесена».
// Если поменялся только title — пишем «переименована».
// Если поменялось время — «с А по B → с C по D».
func formatMoveBody(oldStart, oldEnd, newStart, newEnd time.Time) string {
	if oldStart.Equal(newStart) && oldEnd.Equal(newEnd) {
		return "Встреча переименована."
	}
	return fmt.Sprintf("Перенесена: было %s %s–%s → стало %s %s–%s (UTC).",
		oldStart.Format("02.01"), oldStart.Format("15:04"), oldEnd.Format("15:04"),
		newStart.Format("02.01"), newStart.Format("15:04"), newEnd.Format("15:04"),
	)
}

// updatePushYandex — для одного push: расшифровать токен, рефреш, PUT.
func (s *MeetingProposalService) updatePushYandex(ctx context.Context, empID uuid.UUID, calPath, uid string, in yandex.CreateEventInput) error {
	if s.yandex == nil || s.cipher == nil {
		return errors.New("yandex provider not configured")
	}
	if calPath == "" || uid == "" {
		return errors.New("missing calendar_path or uid")
	}
	integ := s.findYandexIntegration(ctx, empID)
	if integ == nil {
		return errors.New("yandex integration not found")
	}
	access, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil || access == "" {
		return errors.New("cannot decrypt access token")
	}
	refresh := ""
	if integ.RefreshTokenEnc != "" {
		if r, derr := s.cipher.Decrypt(integ.RefreshTokenEnc); derr == nil {
			refresh = r
		}
	}
	tok := &integrations.Token{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "OAuth",
		Raw:          map[string]any{"cal_path": calPath},
	}
	if integ.ExpiresAt != nil {
		tok.Expiry = *integ.ExpiresAt
	}
	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < time.Minute && refresh != "" {
		if newTok, rerr := s.yandex.RefreshToken(ctx, tok); rerr == nil && newTok != nil {
			tok = newTok
		}
	}
	if in.Organizer == "" {
		in.Organizer = integ.AccountEmail
	}
	return s.yandex.UpdateEvent(ctx, tok, calPath, uid, in)
}

// deletePush — расшифровывает токен интеграции и шлёт DELETE в Yandex.
func (s *MeetingProposalService) deletePush(ctx context.Context, empID uuid.UUID, calPath, uid string) error {
	if s.yandex == nil || s.cipher == nil {
		return errors.New("yandex provider not configured")
	}
	if calPath == "" || uid == "" {
		return errors.New("missing calendar_path or uid")
	}
	integ := s.findYandexIntegration(ctx, empID)
	if integ == nil {
		return errors.New("yandex integration not found")
	}
	access, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil || access == "" {
		return errors.New("cannot decrypt access token")
	}
	refresh := ""
	if integ.RefreshTokenEnc != "" {
		if r, derr := s.cipher.Decrypt(integ.RefreshTokenEnc); derr == nil {
			refresh = r
		}
	}
	tok := &integrations.Token{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "OAuth",
		Raw:          map[string]any{"cal_path": calPath},
	}
	if integ.ExpiresAt != nil {
		tok.Expiry = *integ.ExpiresAt
	}
	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < time.Minute && refresh != "" {
		if newTok, rerr := s.yandex.RefreshToken(ctx, tok); rerr == nil && newTok != nil {
			tok = newTok
		}
	}
	return s.yandex.DeleteEvent(ctx, tok, calPath, uid)
}

// --- Подтверждение участия (accept / decline) ---

// IncomingMeeting — приглашение для UI «Входящие приглашения».
type IncomingMeeting struct {
	MeetingID    uuid.UUID `json:"meeting_id"`
	Title        string    `json:"title"`
	StartAt      time.Time `json:"start_at"`
	EndAt        time.Time `json:"end_at"`
	TeamID       *uuid.UUID `json:"team_id,omitempty"`
	TeamName     string    `json:"team_name,omitempty"`
	InitiatorName string   `json:"initiator_name,omitempty"`
	Status       string    `json:"status"`         // pending | accepted | declined
	YandexPushed bool      `json:"yandex_pushed"`  // встреча у меня в Яндексе
	HasYandex    bool      `json:"has_yandex"`     // у меня есть подключённая Yandex-интеграция
	RespondedAt  *time.Time `json:"responded_at,omitempty"`
}

// MeetingResponse — один ответ в выдаче ResponsesFor (видит инициатор/admin).
type MeetingResponse struct {
	EmployeeID   uuid.UUID  `json:"employee_id"`
	FullName     string     `json:"full_name"`
	Status       string     `json:"status"`
	YandexPushed bool       `json:"yandex_pushed"`
	RespondedAt  *time.Time `json:"responded_at,omitempty"`
}

var (
	ErrMeetingResponseNotFound = errors.New("meeting response: not found")
	ErrMeetingResponseInvalid  = errors.New("meeting response: invalid status")
)

// ListIncoming — приглашения для viewerEmp: pending + accepted + declined
// по активным (cancelled_at IS NULL) и будущим (end_at > now()) встречам.
//
// Сортировка: сначала pending (давит на ответ), потом по start_at.
func (s *MeetingProposalService) ListIncoming(ctx context.Context, viewerEmp uuid.UUID) ([]IncomingMeeting, error) {
	if viewerEmp == uuid.Nil {
		return nil, nil
	}

	// Есть ли у меня Yandex — чтобы UI знал, можно ли предлагать «положить в Яндекс».
	hasYandex := s.findYandexIntegration(ctx, viewerEmp) != nil

	rows, err := s.pool.Query(ctx, `
		SELECT mp.id, mp.title, mp.start_at, mp.end_at,
		       mp.team_id, COALESCE(t.name, ''),
		       COALESCE(u.full_name, ''),
		       mr.status::text, mr.yandex_pushed, mr.responded_at
		FROM meeting_responses mr
		JOIN meeting_proposals mp ON mp.id = mr.meeting_id
		LEFT JOIN teams t ON t.id = mp.team_id
		LEFT JOIN users u ON u.id = mp.initiator_user
		WHERE mr.employee_id = $1
		  AND mp.cancelled_at IS NULL
		  AND mp.end_at > now()
		  -- Сам инициатор не получает «приглашение» на свою же встречу.
		  -- Встречу он видит в блоке «Мои встречи» с счётчиком ответов.
		  AND (mp.initiator_emp IS NULL OR mp.initiator_emp <> $1)
		ORDER BY
		  CASE mr.status WHEN 'pending' THEN 0 WHEN 'accepted' THEN 1 ELSE 2 END,
		  mp.start_at
	`, viewerEmp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IncomingMeeting{}
	for rows.Next() {
		var (
			r        IncomingMeeting
			teamID   *uuid.UUID
			teamName string
		)
		if err := rows.Scan(
			&r.MeetingID, &r.Title, &r.StartAt, &r.EndAt,
			&teamID, &teamName,
			&r.InitiatorName,
			&r.Status, &r.YandexPushed, &r.RespondedAt,
		); err != nil {
			continue
		}
		r.TeamID = teamID
		r.TeamName = teamName
		r.HasYandex = hasYandex
		out = append(out, r)
	}
	return out, rows.Err()
}

// Respond — записывает ответ пользователя на приглашение.
//   - accept + pushYandex && HasYandex → PUT в его Yandex (если ещё не было)
//   - decline после accept с push'ом → DELETE из Yandex
//
// Нельзя ответить если встреча отменена или уже прошла.
func (s *MeetingProposalService) Respond(
	ctx context.Context,
	meetingID uuid.UUID,
	viewerEmp uuid.UUID,
	status string,
	pushYandex bool,
) error {
	if status != "accepted" && status != "declined" {
		return ErrMeetingResponseInvalid
	}
	if viewerEmp == uuid.Nil {
		return ErrMeetingForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Текущая запись + состояние встречи.
	var (
		currStatus  string
		currYandex  bool
		mpCancelled *time.Time
		mpEnd       time.Time
		mpStart     time.Time
		mpTitle     string
		mpTeamID    *uuid.UUID
		mpInitiator *uuid.UUID
		mpCategory  *string
	)
	err = tx.QueryRow(ctx, `
		SELECT mr.status::text, mr.yandex_pushed,
		       mp.cancelled_at, mp.end_at, mp.start_at, mp.title,
		       mp.team_id, mp.initiator_user, mp.category
		FROM meeting_responses mr
		JOIN meeting_proposals mp ON mp.id = mr.meeting_id
		WHERE mr.meeting_id = $1 AND mr.employee_id = $2
		FOR UPDATE OF mr
	`, meetingID, viewerEmp).Scan(
		&currStatus, &currYandex,
		&mpCancelled, &mpEnd, &mpStart, &mpTitle,
		&mpTeamID, &mpInitiator, &mpCategory,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMeetingResponseNotFound
		}
		return err
	}
	if mpCancelled != nil {
		return ErrMeetingAlreadyCanceled
	}
	if mpEnd.Before(time.Now()) {
		// Встреча уже прошла — ответы не принимаются.
		return ErrMeetingResponseInvalid
	}
	if currStatus == status && (status == "declined" || currYandex == pushYandex) {
		// Идемпотентность: ровно то же состояние — ничего не делаем.
		return tx.Commit(ctx)
	}

	// Применяем смену статуса.
	if _, err := tx.Exec(ctx, `
		UPDATE meeting_responses
		SET status = $1::meeting_response_status, responded_at = now()
		WHERE meeting_id = $2 AND employee_id = $3
	`, status, meetingID, viewerEmp); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Yandex side-effects (best-effort, не ломаем основной ответ если упало).
	switch status {
	case "accepted":
		if pushYandex && !currYandex {
			// Пуш в его Yandex.
			team, _ := s.teams.ByID(ctx, derefUUIDOrZero(mpTeamID))
			teamName := ""
			if team != nil {
				teamName = team.Name
			}
			initiatorName := ""
			if mpInitiator != nil {
				if u, err := s.users.ByID(ctx, *mpInitiator); err == nil && u != nil {
					initiatorName = u.FullName
				}
			}
			cat := ""
			if mpCategory != nil {
				cat = *mpCategory
			}
			pushed := s.pushYandexForEmployee(ctx, meetingID, viewerEmp, ProposeMeetingInput{
				Title:   mpTitle,
				StartAt: mpStart,
				EndAt:   mpEnd,
			}, teamName, initiatorName, cat)
			if pushed != nil {
				_, _ = s.pool.Exec(ctx, `
					UPDATE meeting_responses SET yandex_pushed = true
					WHERE meeting_id = $1 AND employee_id = $2
				`, meetingID, viewerEmp)
			}
		}
	case "declined":
		if currYandex {
			// Был в Yandex'е — снять.
			_ = s.removeYandexPush(ctx, meetingID, viewerEmp)
			_, _ = s.pool.Exec(ctx, `
				UPDATE meeting_responses SET yandex_pushed = false
				WHERE meeting_id = $1 AND employee_id = $2
			`, meetingID, viewerEmp)
		}
	}

	// Notification инициатору: «X подтвердил/отклонил».
	//
	// Дедупликация: если этот же сотрудник уже отвечал на эту встречу — удаляем
	// прошлое meeting_response-уведомление, чтобы у инициатора оставалось только
	// итоговое состояние (а не вся история «отклонил → подтвердил → отклонил…»).
	if mpInitiator != nil && *mpInitiator != uuid.Nil {
		// Уберём предыдущее уведомление об ответе этого же emp по этой же meeting.
		_, _ = s.pool.Exec(ctx, `
			DELETE FROM notifications
			WHERE user_id = $1
			  AND kind = 'meeting_response'
			  AND COALESCE(payload->>'meeting_id', '') = $2
			  AND COALESCE(payload->>'employee_id', '') = $3
		`, *mpInitiator, meetingID.String(), viewerEmp.String())

		myName := ""
		// Имя viewer'а через emp → user.
		_ = s.pool.QueryRow(ctx, `
			SELECT u.full_name FROM employees e JOIN users u ON u.id = e.user_id
			WHERE e.id = $1
		`, viewerEmp).Scan(&myName)
		verb := "подтвердил"
		if status == "declined" {
			verb = "отклонил"
		}
		body := fmt.Sprintf("%s %s приглашение на %s, %s–%s UTC.",
			myName, verb,
			mpStart.Format("02.01"),
			mpStart.Format("15:04"),
			mpEnd.Format("15:04"),
		)
		_, _ = s.notifications.Push(ctx, CreateInput{
			UserID: *mpInitiator,
			Kind:   "meeting_response",
			Title:  mpTitle,
			Body:   body,
			Link:   "/scheduler",
			Payload: map[string]any{
				"meeting_id":  meetingID.String(),
				"employee_id": viewerEmp.String(),
				"status":      status,
			},
		})
	}
	return nil
}

// removeYandexPush — DELETE события у участника + помечаем meeting_pushes.deleted_at.
func (s *MeetingProposalService) removeYandexPush(ctx context.Context, meetingID, empID uuid.UUID) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source_event_uid, COALESCE(calendar_path, '')
		FROM meeting_pushes
		WHERE meeting_id = $1 AND employee_id = $2 AND deleted_at IS NULL
	`, meetingID, empID)
	if err != nil {
		return err
	}
	type row struct {
		id      uuid.UUID
		uid     string
		calPath string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.uid, &r.calPath); err == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	for _, r := range list {
		if err := s.deletePush(ctx, empID, r.calPath, r.uid); err == nil {
			_, _ = s.pool.Exec(ctx, `
				UPDATE meeting_pushes SET deleted_at = now()
				WHERE id = $1
			`, r.id)
		}
	}
	return nil
}

// ResponsesFor — список всех ответов по встрече. Видит инициатор / Manager
// (owner команды) / Admin / HR.
func (s *MeetingProposalService) ResponsesFor(
	ctx context.Context,
	meetingID uuid.UUID,
	viewerUser uuid.UUID,
	viewerEmp uuid.UUID,
	role domain.Role,
) ([]MeetingResponse, error) {
	// RBAC: читаем initiator + team owner.
	var (
		initiator *uuid.UUID
		teamID    *uuid.UUID
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT initiator_user, team_id FROM meeting_proposals WHERE id = $1
	`, meetingID).Scan(&initiator, &teamID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMeetingNotFound
		}
		return nil, err
	}
	isAdmin := role == domain.RoleAdmin || role == domain.RoleHR
	isInitiator := initiator != nil && *initiator == viewerUser
	isManager := role == domain.RoleManager || role == domain.RolePM
	isOwner := false
	if isManager && teamID != nil {
		var ownerID *uuid.UUID
		_ = s.pool.QueryRow(ctx, `SELECT owner_id FROM teams WHERE id = $1`, *teamID).Scan(&ownerID)
		if ownerID != nil && *ownerID == viewerEmp {
			isOwner = true
		}
	}
	if !isAdmin && !isInitiator && !isOwner {
		return nil, ErrMeetingForbidden
	}

	rows, err := s.pool.Query(ctx, `
		SELECT mr.employee_id, COALESCE(u.full_name, '?'),
		       mr.status::text, mr.yandex_pushed, mr.responded_at
		FROM meeting_responses mr
		JOIN employees e ON e.id = mr.employee_id
		JOIN users u ON u.id = e.user_id
		WHERE mr.meeting_id = $1
		ORDER BY
		  CASE mr.status WHEN 'accepted' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END,
		  u.full_name
	`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MeetingResponse{}
	for rows.Next() {
		var r MeetingResponse
		if err := rows.Scan(&r.EmployeeID, &r.FullName, &r.Status, &r.YandexPushed, &r.RespondedAt); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// derefUUIDOrZero — для nil-указателя возвращает uuid.Nil.
func derefUUIDOrZero(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

// --- Проверка конфликтов перед ручным созданием встречи ---

// ConflictInfo — один кусок «занятости» сотрудника в заданном слоте.
//
// Kind различает тип:
//   - "meeting"        — пересекающаяся встреча из calendar_events
//   - "exception"      — отпуск/больничный/командировка/etc
//   - "outside_hours"  — слот целиком или частично вне рабочих часов сотрудника
//                        (включая попадание на выходной день по его графику)
type ConflictInfo struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	FullName   string    `json:"full_name"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	StartAt    time.Time `json:"start_at"`
	EndAt      time.Time `json:"end_at"`
}

// CheckConflicts — для заданного временного слота возвращает список конфликтов
// по каждому сотруднику. Используется UI «Создать вручную», чтобы инициатор
// видел кто из приглашённых уже занят, и не блокировал создание (мягкое
// предупреждение, не валидатор).
//
// Что считается конфликтом:
//   - Активное событие в calendar_events (status != cancelled, is_excluded = false),
//     пересекающееся по времени, КРОМЕ task-блоков планировщика
//     (Фокус-время / Задача / План задачи — их планировщик подвинет автоматически).
//   - Активное time_exception (отпуск/больничный/командировка), перекрывающее слот.
//
// Дедупликация по emp_id не нужна — в одном слоте у человека может быть несколько
// событий, все их полезно показать.
func (s *MeetingProposalService) CheckConflicts(
	ctx context.Context,
	startAt, endAt time.Time,
	empIDs []uuid.UUID,
) ([]ConflictInfo, error) {
	if !endAt.After(startAt) {
		return nil, ErrMeetingInvalidRange
	}
	// Фильтр пустого списка — без выборок.
	cleaned := make([]uuid.UUID, 0, len(empIDs))
	for _, id := range empIDs {
		if id != uuid.Nil {
			cleaned = append(cleaned, id)
		}
	}
	if len(cleaned) == 0 {
		return []ConflictInfo{}, nil
	}

	out := make([]ConflictInfo, 0, 8)

	// 1) Календарные события (без task-блоков).
	rows, err := s.pool.Query(ctx, `
		SELECT ce.employee_id,
		       COALESCE(u.full_name, ''),
		       COALESCE(ce.title, ''),
		       ce.start_at, ce.end_at
		FROM calendar_events ce
		JOIN employees e ON e.id = ce.employee_id
		JOIN users u ON u.id = e.user_id
		WHERE ce.employee_id = ANY($1)
		  AND ce.is_excluded = false
		  AND ce.status <> 'cancelled'
		  AND ce.start_at < $3
		  AND ce.end_at > $2
		  AND (ce.category IS NULL
		       OR ce.category NOT IN ('Фокус-время', 'Задача', 'План задачи'))
	`, cleaned, startAt.UTC(), endAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("check conflicts events: %w", err)
	}
	for rows.Next() {
		var c ConflictInfo
		if err := rows.Scan(&c.EmployeeID, &c.FullName, &c.Title, &c.StartAt, &c.EndAt); err != nil {
			continue
		}
		c.Kind = "meeting"
		if c.Title == "" {
			c.Title = "Встреча"
		}
		out = append(out, c)
	}
	rows.Close()

	// 2) Исключения (отпуск/больничный/командировка) — kind хранится строкой.
	rows2, err := s.pool.Query(ctx, `
		SELECT te.employee_id,
		       COALESCE(u.full_name, ''),
		       te.kind::text,
		       COALESCE(te.comment, ''),
		       te.start_at, te.end_at
		FROM time_exceptions te
		JOIN employees e ON e.id = te.employee_id
		JOIN users u ON u.id = e.user_id
		WHERE te.employee_id = ANY($1)
		  AND te.start_at < $3
		  AND te.end_at > $2
	`, cleaned, startAt.UTC(), endAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("check conflicts exceptions: %w", err)
	}
	for rows2.Next() {
		var (
			c       ConflictInfo
			kind    string
			comment string
		)
		if err := rows2.Scan(&c.EmployeeID, &c.FullName, &kind, &comment, &c.StartAt, &c.EndAt); err != nil {
			continue
		}
		c.Kind = "exception"
		c.Title = exceptionTitle(kind, comment)
		out = append(out, c)
	}
	rows2.Close()

	// 3) Проверка рабочих часов: для каждого сотрудника тянем активный
	// work_profile, переводим start/end в его TZ, смотрим попадает ли в
	// окно рабочего дня. Если нет — это «вне графика» (включая выходной).
	//
	// Тащим имена одним запросом — иначе для каждого emp пришлось бы делать
	// отдельный SELECT u.full_name.
	nameRows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(u.full_name, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, cleaned)
	if err == nil {
		names := map[uuid.UUID]string{}
		for nameRows.Next() {
			var id uuid.UUID
			var n string
			if err := nameRows.Scan(&id, &n); err == nil {
				names[id] = n
			}
		}
		nameRows.Close()

		for _, empID := range cleaned {
			profile, err := s.profiles.Active(ctx, empID)
			if err != nil || profile == nil {
				// Нет профиля → ничего не знаем про график → не считаем конфликтом.
				continue
			}
			if oh := workHoursConflict(empID, names[empID], startAt, endAt, profile); oh != nil {
				out = append(out, *oh)
			}
		}
	}

	return out, nil
}

// workHoursConflict — проверяет, попадает ли слот [startAt, endAt) в рабочее
// окно сотрудника по его профилю. Возвращает nil если всё ок, иначе
// ConflictInfo c kind="outside_hours" и понятным title (выходной/часы).
func workHoursConflict(empID uuid.UUID, name string, startAt, endAt time.Time, profile *domain.WorkProfile) *ConflictInfo {
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	s := startAt.In(loc)
	e := endAt.In(loc)

	var dh *domain.DayHours
	switch s.Weekday() {
	case time.Monday:
		dh = profile.DaysOfWeek.Mon
	case time.Tuesday:
		dh = profile.DaysOfWeek.Tue
	case time.Wednesday:
		dh = profile.DaysOfWeek.Wed
	case time.Thursday:
		dh = profile.DaysOfWeek.Thu
	case time.Friday:
		dh = profile.DaysOfWeek.Fri
	case time.Saturday:
		dh = profile.DaysOfWeek.Sat
	case time.Sunday:
		dh = profile.DaysOfWeek.Sun
	}
	if dh == nil {
		// Выходной по графику.
		return &ConflictInfo{
			EmployeeID: empID,
			FullName:   name,
			Kind:       "outside_hours",
			Title:      "Выходной день по графику",
			StartAt:    startAt,
			EndAt:      endAt,
		}
	}
	// Парсим HH:MM и собираем рабочее окно в этот же день и TZ.
	ws, err1 := time.ParseInLocation("15:04", dh.Start, loc)
	we, err2 := time.ParseInLocation("15:04", dh.End, loc)
	if err1 != nil || err2 != nil {
		return nil
	}
	workStart := time.Date(s.Year(), s.Month(), s.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
	workEnd := time.Date(s.Year(), s.Month(), s.Day(), we.Hour(), we.Minute(), 0, 0, loc)
	if s.Before(workStart) || e.After(workEnd) {
		return &ConflictInfo{
			EmployeeID: empID,
			FullName:   name,
			Kind:       "outside_hours",
			Title:      "Вне рабочих часов (" + dh.Start + "–" + dh.End + ")",
			StartAt:    startAt,
			EndAt:      endAt,
		}
	}
	return nil
}

// exceptionTitle — превращает kind+comment в человекочитаемый заголовок.
func exceptionTitle(kind, comment string) string {
	label := map[string]string{
		"vacation":      "Отпуск",
		"sick":          "Больничный",
		"business_trip": "Командировка",
		"personal":      "Личный день",
		"custom":        "Недоступен",
	}[kind]
	if label == "" {
		label = "Недоступен"
	}
	if comment != "" {
		return label + " — " + comment
	}
	return label
}

// --- Анти-burnout: ограничение на количество часов встреч в неделю ---

// WeeklyMeetingHoursLimit — порог, после которого создание новой встречи
// требует подтверждения от инициатора (soft-block). Считаются только реальные
// встречи (не task-блоки и не фокус-время) за календарную неделю Пн-Вс в TZ
// сотрудника.
const WeeklyMeetingHoursLimit = 35.0

// OverloadEntry — один сотрудник, у которого после новой встречи количество
// часов встреч в неделе превысит порог.
type OverloadEntry struct {
	EmployeeID     uuid.UUID `json:"employee_id"`
	FullName       string    `json:"full_name"`
	CurrentHours   float64   `json:"current_hours"`   // часы встреч в этой неделе сейчас (без новой)
	ProjectedHours float64   `json:"projected_hours"` // current + длительность новой
	Limit          float64   `json:"limit"`           // порог
	WeekStart      time.Time `json:"week_start"`      // начало недели Пн в TZ сотрудника, UTC
	WeekEnd        time.Time `json:"week_end"`        // конец недели Пн (Пн+7d) в TZ, UTC
}

// CheckOverload — для каждого emp_id считает суммарную длительность встреч
// за календарную неделю (Пн 00:00 — Пн 00:00 в TZ сотрудника), куда попадает
// новая встреча, и прибавляет её длительность. Если итого > порога —
// возвращает запись об этом сотруднике.
//
// Учитываются только реальные встречи: status != 'cancelled', is_excluded=false,
// category НЕ в (Фокус-время, Задача, План задачи).
func (s *MeetingProposalService) CheckOverload(
	ctx context.Context,
	startAt, endAt time.Time,
	empIDs []uuid.UUID,
) ([]OverloadEntry, error) {
	if !endAt.After(startAt) {
		return nil, ErrMeetingInvalidRange
	}
	newDurHours := endAt.Sub(startAt).Hours()
	if newDurHours <= 0 || len(empIDs) == 0 {
		return []OverloadEntry{}, nil
	}

	// Резолвим имена сотрудников одним запросом — для красивого ответа.
	cleaned := make([]uuid.UUID, 0, len(empIDs))
	for _, id := range empIDs {
		if id != uuid.Nil {
			cleaned = append(cleaned, id)
		}
	}
	if len(cleaned) == 0 {
		return []OverloadEntry{}, nil
	}

	names := map[uuid.UUID]string{}
	if nameRows, nerr := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(u.full_name, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, cleaned); nerr == nil {
		for nameRows.Next() {
			var id uuid.UUID
			var n string
			if err := nameRows.Scan(&id, &n); err == nil {
				names[id] = n
			}
		}
		nameRows.Close()
	}

	out := make([]OverloadEntry, 0, 2)

	for _, empID := range cleaned {
		// Определяем неделю в TZ сотрудника. Без профиля — UTC.
		loc := time.UTC
		if profile, perr := s.profiles.Active(ctx, empID); perr == nil && profile != nil {
			if l, lerr := time.LoadLocation(profile.Timezone); lerr == nil && l != nil {
				loc = l
			}
		}
		weekStart, weekEnd := weekBoundaries(startAt, loc)

		// Сумма часов встреч в [weekStart, weekEnd), кроме task-блоков.
		// Учитываем пересечение с границами недели: если встреча начинается
		// до weekStart или заканчивается после weekEnd — считаем только ту часть,
		// что внутри недели. Это редкий случай, но честнее.
		var minutes float64
		err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(
				EXTRACT(EPOCH FROM (
					LEAST(end_at, $3::timestamptz) - GREATEST(start_at, $2::timestamptz)
				)) / 60
			), 0)::float8
			FROM calendar_events
			WHERE employee_id = $1
			  AND is_excluded = false
			  AND status <> 'cancelled'
			  AND start_at < $3::timestamptz
			  AND end_at > $2::timestamptz
			  AND (category IS NULL
			       OR category NOT IN ('Фокус-время', 'Задача', 'План задачи'))
		`, empID, weekStart, weekEnd).Scan(&minutes)
		if err != nil {
			// Не валим всю проверку из-за одного сотрудника — просто пропустим.
			continue
		}
		currentHours := minutes / 60
		projected := currentHours + newDurHours
		if projected > WeeklyMeetingHoursLimit {
			out = append(out, OverloadEntry{
				EmployeeID:     empID,
				FullName:       names[empID],
				CurrentHours:   round1(currentHours),
				ProjectedHours: round1(projected),
				Limit:          WeeklyMeetingHoursLimit,
				WeekStart:      weekStart,
				WeekEnd:        weekEnd,
			})
		}
	}

	return out, nil
}

// weekBoundaries — для момента t в TZ loc возвращает границы недели
// [Понедельник 00:00, Понедельник+7d 00:00) в UTC. Так удобно сравнивать
// с UTC-значениями timestamptz в БД.
func weekBoundaries(t time.Time, loc *time.Location) (time.Time, time.Time) {
	local := t.In(loc)
	wd := int(local.Weekday())
	// Воскресенье = 0 в Go-стандарте → у нас Пн-Вс, делаем Пн=1, Вс=7.
	if wd == 0 {
		wd = 7
	}
	mondayLocal := time.Date(
		local.Year(), local.Month(), local.Day()-(wd-1),
		0, 0, 0, 0, loc,
	)
	return mondayLocal.UTC(), mondayLocal.Add(7 * 24 * time.Hour).UTC()
}

func round1(x float64) float64 {
	return float64(int(x*10+0.5)) / 10
}

// --- Умный анализ «какую встречу перенести» ---

// SuggestedReschedule — кандидат на перенос с обоснованием.
type SuggestedReschedule struct {
	MeetingID uuid.UUID `json:"meeting_id"`
	Title     string    `json:"title"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	TeamName  string    `json:"team_name,omitempty"`
	Category  string    `json:"category,omitempty"`
	Score     int       `json:"score"`
	Reasons   []string  `json:"reasons"`
}

// SuggestReschedule — для активных встреч viewer'а (где он может переносить)
// в горизонте N дней считает «score переноса» по нескольким сигналам и
// возвращает top кандидатов с человекочитаемыми причинами.
//
// Учитываемые сигналы (накапливаются в score):
//   - +30 за каждого участника, у которого в день встречи >5ч встреч (перегруз дня).
//   - +50 если встреча сама пересекается по времени с другой моей активной встречей
//     (double-booking).
//   - +20 если в моём расписании в день встречи есть 3+ встречи в окне +-1.5ч от неё
//     (нет перерыва).
//   - +15 если category in ('Стендапы', '1:1') — такие встречи переносятся легче.
//
// Возвращает не больше topN записей, отсортированных по score desc.
// Если score=0 — встречу всё равно не вернёт, мы показываем только осмысленных
// кандидатов.
func (s *MeetingProposalService) SuggestReschedule(
	ctx context.Context,
	viewerUser, viewerEmp uuid.UUID,
	role domain.Role,
	days, topN int,
) ([]SuggestedReschedule, error) {
	if days <= 0 {
		days = 7
	}
	if topN <= 0 {
		topN = 3
	}

	// 1. Берём мои встречи на ближайшие N дней — те где я могу переносить
	// (инициатор или owner команды или admin). Переиспользуем ListMy.
	mine, err := s.ListMy(ctx, viewerUser, viewerEmp, role)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	candidates := make([]MyMeeting, 0, len(mine))
	for _, m := range mine {
		if !m.CanCancel {
			continue
		}
		if m.StartAt.After(cutoff) {
			continue
		}
		if m.EndAt.Before(time.Now()) {
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return []SuggestedReschedule{}, nil
	}

	// 2. Для каждой кандидатуры тянем участников и категорию.
	type meetingData struct {
		m        MyMeeting
		category string
		empIDs   []uuid.UUID
	}
	dataByID := make(map[uuid.UUID]*meetingData, len(candidates))
	meetingIDs := make([]uuid.UUID, 0, len(candidates))
	for _, m := range candidates {
		dataByID[m.ID] = &meetingData{m: m}
		meetingIDs = append(meetingIDs, m.ID)
	}

	// Категория встречи из meeting_proposals.
	if rows, qerr := s.pool.Query(ctx, `
		SELECT id, COALESCE(category, '') FROM meeting_proposals
		WHERE id = ANY($1)
	`, meetingIDs); qerr == nil {
		for rows.Next() {
			var id uuid.UUID
			var cat string
			if err := rows.Scan(&id, &cat); err == nil {
				if d := dataByID[id]; d != nil {
					d.category = cat
				}
			}
		}
		rows.Close()
	}

	// Участники: emp_id для каждого приглашённого. ListMy уже даёт accepted/
	// declined/pending — но нам нужны emp_id, лезем в meeting_responses.
	if rows, qerr := s.pool.Query(ctx, `
		SELECT meeting_id, employee_id FROM meeting_responses
		WHERE meeting_id = ANY($1) AND status <> 'declined'
	`, meetingIDs); qerr == nil {
		for rows.Next() {
			var mid, eid uuid.UUID
			if err := rows.Scan(&mid, &eid); err == nil {
				if d := dataByID[mid]; d != nil {
					d.empIDs = append(d.empIDs, eid)
				}
			}
		}
		rows.Close()
	}

	// 3. Подгружаем все остальные мои встречи (не только candidates) того же
	// horizon, чтобы детектить «3 встречи подряд» и double-booking.
	mySlots := make([]struct{ start, end time.Time }, 0, len(candidates))
	for _, m := range candidates {
		mySlots = append(mySlots, struct{ start, end time.Time }{m.StartAt, m.EndAt})
	}

	// 4. Оцениваем каждый кандидат.
	out := make([]SuggestedReschedule, 0, len(candidates))
	for _, m := range candidates {
		d := dataByID[m.ID]
		score := 0
		reasons := []string{}

		// 4a. Перегруз дня участника.
		overloadedCount := s.countDayOverloadedParticipants(ctx, m.StartAt, d.empIDs, m.ID)
		if overloadedCount > 0 {
			score += 30 * overloadedCount
			reasons = append(reasons, fmt.Sprintf("у %d %s этот день перегружен встречами",
				overloadedCount, pluralRu(overloadedCount, "участника", "участников", "участников")))
		}

		// 4b. Double-booking — пересечение с другой моей встречей.
		hasOverlap := false
		for _, s := range mySlots {
			if s.start.Equal(m.StartAt) && s.end.Equal(m.EndAt) {
				continue // та же встреча
			}
			if m.StartAt.Before(s.end) && s.start.Before(m.EndAt) {
				hasOverlap = true
				break
			}
		}
		if hasOverlap {
			score += 50
			reasons = append(reasons, "пересекается с другой твоей встречей")
		}

		// 4c. «Нет перерыва» — 3+ встречи в окне +-1.5ч от этой.
		windowStart := m.StartAt.Add(-90 * time.Minute)
		windowEnd := m.EndAt.Add(90 * time.Minute)
		neighborhood := 0
		for _, s := range mySlots {
			if s.start.Before(windowEnd) && windowStart.Before(s.end) {
				neighborhood++
			}
		}
		if neighborhood >= 3 {
			score += 20
			reasons = append(reasons, "в этот промежуток 3+ встречи подряд без перерыва")
		}

		// 4d. Легко переносимая категория.
		switch d.category {
		case "Стендапы", "1:1":
			score += 15
			reasons = append(reasons, "тип встречи легко переносится")
		}

		if score == 0 {
			continue
		}
		out = append(out, SuggestedReschedule{
			MeetingID: m.ID,
			Title:     m.Title,
			StartAt:   m.StartAt,
			EndAt:     m.EndAt,
			TeamName:  m.TeamName,
			Category:  d.category,
			Score:     score,
			Reasons:   reasons,
		})
	}

	// 5. Сортировка по score desc.
	sortSuggested(out)
	if len(out) > topN {
		out = out[:topN]
	}
	return out, nil
}

// countDayOverloadedParticipants — для каждого участника emp_id смотрит
// сколько у него часов встреч в день этой встречи (в TZ сотрудника), кроме
// task-блоков и кроме оцениваемой встречи. Если > 5ч — считаем перегруженным.
// Возвращает количество таких участников.
func (s *MeetingProposalService) countDayOverloadedParticipants(
	ctx context.Context,
	startAt time.Time,
	empIDs []uuid.UUID,
	excludeMeetingID uuid.UUID,
) int {
	if len(empIDs) == 0 {
		return 0
	}
	const dayOverloadThresholdHours = 5.0
	overloaded := 0
	for _, empID := range empIDs {
		loc := time.UTC
		if profile, perr := s.profiles.Active(ctx, empID); perr == nil && profile != nil {
			if l, lerr := time.LoadLocation(profile.Timezone); lerr == nil && l != nil {
				loc = l
			}
		}
		local := startAt.In(loc)
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		dayEnd := dayStart.Add(24 * time.Hour)

		var minutes float64
		err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(
				EXTRACT(EPOCH FROM (
					LEAST(end_at, $3::timestamptz) - GREATEST(start_at, $2::timestamptz)
				)) / 60
			), 0)::float8
			FROM calendar_events ce
			WHERE ce.employee_id = $1
			  AND ce.is_excluded = false
			  AND ce.status <> 'cancelled'
			  AND ce.start_at < $3::timestamptz
			  AND ce.end_at > $2::timestamptz
			  AND (ce.category IS NULL
			       OR ce.category NOT IN ('Фокус-время', 'Задача', 'План задачи'))
			  AND NOT (ce.source_event_id LIKE 'meeting-' || $4::text || '-%')
		`, empID, dayStart.UTC(), dayEnd.UTC(), excludeMeetingID).Scan(&minutes)
		if err != nil {
			continue
		}
		if minutes/60 > dayOverloadThresholdHours {
			overloaded++
		}
	}
	return overloaded
}

// pluralRu — выбирает правильную форму русского слова по числу.
// pluralRu(1, "час", "часа", "часов") → "час"
// pluralRu(2, "час", "часа", "часов") → "часа"
// pluralRu(5, "час", "часа", "часов") → "часов"
// pluralRu(21, ...) → "час", pluralRu(22, ...) → "часа", и т.д.
func pluralRu(n int, one, few, many string) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	m10 := abs % 10
	m100 := abs % 100
	if m10 == 1 && m100 != 11 {
		return one
	}
	if m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) {
		return few
	}
	return many
}

// sortSuggested — in-place сортировка по score desc, при равенстве — по start_at asc.
func sortSuggested(arr []SuggestedReschedule) {
	for i := 1; i < len(arr); i++ {
		for j := i; j > 0; j-- {
			if arr[j].Score > arr[j-1].Score ||
				(arr[j].Score == arr[j-1].Score && arr[j].StartAt.Before(arr[j-1].StartAt)) {
				arr[j], arr[j-1] = arr[j-1], arr[j]
				continue
			}
			break
		}
	}
}
