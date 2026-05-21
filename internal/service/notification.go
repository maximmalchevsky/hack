package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"worktimesync/internal/domain"
	"worktimesync/internal/notify"
	"worktimesync/internal/repository"
)

const (
	// WSNotificationsChannel — Redis pub/sub-канал для рассылки уведомлений
	// конкретному пользователю. Формат ключа: ws:user:<user_id>.
	WSNotificationsChannel = "ws:user:"
)

// NotificationService — создание + чтение + публикация в Redis pub/sub.
// Дополнительные каналы доставки (email, telegram) подключаются через
// WithTransports — best-effort, ошибки транспортов не валят основной flow.
type NotificationService struct {
	pool       *pgxpool.Pool
	repo       *repository.NotificationRepo
	redis      *redis.Client
	transports []notify.Transport
}

func NewNotificationService(pool *pgxpool.Pool, rdb *redis.Client) *NotificationService {
	return &NotificationService{
		pool:  pool,
		repo:  repository.NewNotificationRepo(pool),
		redis: rdb,
	}
}

// WithTransports — DI: подключаем дополнительные каналы (email, telegram).
// Можно вызывать с nil-ами, отфильтруем.
func (s *NotificationService) WithTransports(ts ...notify.Transport) *NotificationService {
	for _, t := range ts {
		if t == nil {
			continue
		}
		if ec, ok := t.(notify.EnabledChecker); ok && !ec.Enabled() {
			continue
		}
		s.transports = append(s.transports, t)
	}
	return s
}

// CreateInput — параметры нового уведомления.
type CreateInput struct {
	UserID  uuid.UUID
	Kind    string
	Title   string
	Body    string
	Link    string
	Payload map[string]any
}

// Push — сохраняет уведомление в БД и публикует в Redis для WS-доставки.
func (s *NotificationService) Push(ctx context.Context, in CreateInput) (*domain.Notification, error) {
	var raw []byte
	if in.Payload != nil {
		raw, _ = json.Marshal(in.Payload)
	}
	n, err := s.repo.Create(ctx, repository.CreateNotificationInput{
		UserID:  in.UserID,
		Kind:    in.Kind,
		Title:   in.Title,
		Body:    in.Body,
		Link:    in.Link,
		Payload: raw,
	})
	if err != nil {
		return nil, err
	}

	// Публикация для активных WS-сессий пользователя.
	if s.redis != nil {
		msg, _ := json.Marshal(map[string]any{
			"type":         "notification.created",
			"notification": notificationToMap(*n),
		})
		_ = s.redis.Publish(ctx, WSNotificationsChannel+in.UserID.String(), msg).Err()
	}

	// Дополнительные каналы — email/telegram. Запускаем в горутине, чтобы
	// не блокировать ответ HTTP-запроса и не валить основной поток.
	if len(s.transports) > 0 {
		go s.dispatchToTransports(in.UserID, *n)
	}

	return n, nil
}

// dispatchToTransports — выполняется в отдельной горутине.
// Получает свежий контекст (background), потому что родительский может быть
// уже отменён к моменту отправки.
func (s *NotificationService) dispatchToTransports(userID uuid.UUID, n domain.Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), 15_000_000_000) // 15s
	defer cancel()

	// Подгружаем prefs пользователя одним запросом.
	var (
		email, name string
		emailOn     bool
		tgChat      *string
		tgOn        bool
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT u.email, u.full_name,
		       u.email_notifications, u.telegram_chat_id, u.telegram_notifications
		FROM users u WHERE u.id = $1
	`, userID).Scan(&email, &name, &emailOn, &tgChat, &tgOn); err != nil {
		log.Warn().Err(err).Str("user_id", userID.String()).Msg("notify: load prefs failed")
		return
	}

	tg := ""
	if tgChat != nil {
		tg = *tgChat
	}

	msg := notify.Message{
		UserID:     userID.String(),
		UserName:   name,
		UserEmail:  email,
		TelegramID: tg,
		Title:      n.Title,
		Body:       n.Body,
		Link:       n.Link,
		Kind:       n.Kind,
	}

	for _, t := range s.transports {
		switch t.Name() {
		case "email":
			if !emailOn || email == "" {
				continue
			}
		case "telegram":
			if !tgOn || tg == "" {
				continue
			}
		}
		if err := t.Send(ctx, msg); err != nil {
			log.Warn().Err(err).Str("transport", t.Name()).Str("kind", n.Kind).Msg("notify: send failed")
		}
	}
}

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, onlyUnread bool) ([]domain.Notification, error) {
	return s.repo.ListByUser(ctx, userID, onlyUnread, 50)
}

func (s *NotificationService) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	err := s.repo.MarkRead(ctx, id, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return errors.New("notification: not found or already read")
	}
	return err
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.repo.MarkAllRead(ctx, userID)
}

func notificationToMap(n domain.Notification) map[string]any {
	m := map[string]any{
		"id":         n.ID,
		"kind":       n.Kind,
		"title":      n.Title,
		"created_at": n.CreatedAt,
	}
	if n.Body != "" {
		m["body"] = n.Body
	}
	if n.Link != "" {
		m["link"] = n.Link
	}
	return m
}
