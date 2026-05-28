package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	tele "gopkg.in/telebot.v3"
)

type PulseSubmitter interface {
	SubmitFromBot(ctx context.Context, empID uuid.UUID, score int) error
}

type Bot struct {
	pool  *pgxpool.Pool
	log   zerolog.Logger
	bot   *tele.Bot
	pulse PulseSubmitter
}

func NewBot(token string, pool *pgxpool.Pool, log zerolog.Logger) (*Bot, error) {
	if token == "" {
		return nil, nil
	}
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: func(err error, c tele.Context) {
			log.Warn().Err(err).Msg("telebot: handler error")
		},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("telebot init: %w", err)
	}

	bot := &Bot{pool: pool, log: log, bot: b}

	b.Handle("/start", bot.onStart)
	b.Handle("/stop", bot.onStop)
	b.Handle("/help", bot.onHelp)
	b.Handle("/today", bot.onToday)
	b.Handle("/tomorrow", bot.onTomorrow)
	b.Handle("/pulse", bot.onPulse)
	b.Handle("/team", bot.onTeam)
	b.Handle(tele.OnCallback, bot.onCallback)
	b.Handle(tele.OnText, bot.onText)

	return bot, nil
}

func (b *Bot) WithPulse(p PulseSubmitter) *Bot {
	if b == nil {
		return b
	}
	b.pulse = p
	return b
}

func (b *Bot) Run(ctx context.Context) {
	if b == nil {
		return
	}
	go func() {
		<-ctx.Done()
		b.bot.Stop()
	}()
	b.log.Info().Str("username", b.bot.Me.Username).Msg("telegram bot started")
	b.bot.Start()
	b.log.Info().Msg("telegram bot stopped")
}

func (b *Bot) onStart(c tele.Context) error {
	payload := strings.TrimSpace(c.Message().Payload)
	if payload == "" {
		return c.Send(
			"Привет! Это бот Workie.\n\n" +
				"Чтобы получать уведомления о встречах и подтверждениях, открой " +
				"свой профиль в системе → раздел «Каналы уведомлений» → " +
				"«Подключить Telegram». Ссылка из системы привяжет твой аккаунт.",
		)
	}
	userID, err := uuid.Parse(payload)
	if err != nil {
		return c.Send("Не получилось распознать payload. Открой ссылку из своего профиля заново.")
	}

	chatID := c.Sender().ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var email string
	if err := b.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		b.log.Warn().Err(err).Str("user_id", userID.String()).Msg("telegram bind: user not found")
		return c.Send("Пользователь не найден в системе. Перезайди в Workie и попробуй снова.")
	}

	if _, err := b.pool.Exec(ctx, `
		UPDATE users SET telegram_chat_id = $1, telegram_notifications = true
		WHERE id = $2
	`, fmt.Sprintf("%d", chatID), userID); err != nil {
		b.log.Error().Err(err).Msg("telegram bind: update failed")
		return c.Send("Не получилось сохранить привязку. Попробуй позже.")
	}

	b.log.Info().Int64("chat_id", chatID).Str("user_id", userID.String()).Msg("telegram bound to user")
	return c.Send(fmt.Sprintf(
		"Готово! Аккаунт %s подключён. Теперь сюда будут приходить уведомления о встречах, переносах и подтверждениях.\n\n"+
			"Команды:\n• /stop — отключить уведомления\n• /help — помощь",
		email,
	))
}

func (b *Bot) onStop(c tele.Context) error {
	chatID := c.Sender().ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tag, err := b.pool.Exec(ctx, `
		UPDATE users SET telegram_chat_id = NULL, telegram_notifications = false
		WHERE telegram_chat_id = $1
	`, fmt.Sprintf("%d", chatID))
	if err != nil {
		return c.Send("Не получилось отключить — попробуй позже.")
	}
	if tag.RowsAffected() == 0 {
		return c.Send("Этот чат не был привязан ни к одному пользователю.")
	}
	return c.Send("Уведомления в Telegram отключены. Подключить снова можно из профиля в системе.")
}

func (b *Bot) onHelp(c tele.Context) error {
	return c.Send(
		"Workie · бот уведомлений.\n\n" +
			"Команды:\n" +
			"• /today — встречи на сегодня\n" +
			"• /tomorrow — встречи на завтра\n" +
			"• /pulse — ответить на 2-недельный опрос\n" +
			"• /team — сводка команды (для руководителя)\n" +
			"• /stop — отключить уведомления\n" +
			"• /help — это сообщение",
	)
}

func (b *Bot) onText(c tele.Context) error {
	return c.Send(
		"Я только пересылаю уведомления из Workie.\n" +
			"Для управления — заходи в систему: /help, /stop.",
	)
}

func (b *Bot) Username() string {
	if b == nil || b.bot == nil {
		return ""
	}
	return b.bot.Me.Username
}

var ErrBotDisabled = errors.New("telegram bot disabled")
