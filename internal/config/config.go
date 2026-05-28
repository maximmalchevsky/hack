package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	App      App
	HTTP     HTTP
	Postgres Postgres
	Redis    Redis
	JWT      JWT
	GigaChat GigaChat
	OAuth    OAuth
	Risk     Risk
	Asynq    Asynq
	CORS     CORS
	SMTP     SMTP
	Telegram Telegram
	IMIP     IMIP
}

type IMIP struct {
	Enabled      bool          `env:"IMIP_ENABLED" envDefault:"false"`
	ReplyTo      string        `env:"IMIP_REPLY_TO"`
	IMAPHost     string        `env:"IMAP_HOST"`
	IMAPPort     int           `env:"IMAP_PORT" envDefault:"993"`
	IMAPUser     string        `env:"IMAP_USER"`
	IMAPPass     string        `env:"IMAP_PASS"`
	IMAPMailbox  string        `env:"IMAP_MAILBOX" envDefault:"INBOX"`
	PollInterval time.Duration `env:"IMAP_POLL_INTERVAL" envDefault:"60s"`
}

type SMTP struct {
	Host     string `env:"SMTP_HOST"`
	Port     int    `env:"SMTP_PORT" envDefault:"587"`
	User     string `env:"SMTP_USER"`
	Pass     string `env:"SMTP_PASS"`
	From     string `env:"SMTP_FROM"`
	StartTLS bool   `env:"SMTP_STARTTLS" envDefault:"true"`
}

type Telegram struct {
	BotToken    string `env:"TELEGRAM_BOT_TOKEN"`
	BotUsername string `env:"TELEGRAM_BOT_USERNAME"`
}

type App struct {
	Env       string `env:"APP_ENV" envDefault:"development"`
	PublicURL string `env:"APP_PUBLIC_URL" envDefault:"http://localhost:8080"`

	WebURL        string `env:"APP_WEB_URL" envDefault:"http://localhost:5173"`
	EncryptionKey string `env:"APP_ENCRYPTION_KEY"`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
}

type HTTP struct {
	Port            int           `env:"APP_PORT" envDefault:"8080"`
	ReadTimeout     time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout    time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"15s"`
}

type Postgres struct {
	URL             string        `env:"DATABASE_URL,required"`
	MaxConns        int32         `env:"DATABASE_MAX_CONNS" envDefault:"20"`
	MinConns        int32         `env:"DATABASE_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
}

type Redis struct {
	URL      string `env:"REDIS_URL,required"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
}

type JWT struct {
	Secret     string        `env:"JWT_SECRET,required"`
	AccessTTL  time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
	RefreshTTL time.Duration `env:"JWT_REFRESH_TTL" envDefault:"168h"`
}

type GigaChat struct {
	ClientID     string `env:"GIGACHAT_CLIENT_ID"`
	ClientSecret string `env:"GIGACHAT_CLIENT_SECRET"`
	Scope        string `env:"GIGACHAT_SCOPE" envDefault:"GIGACHAT_API_PERS"`
	APIURL       string `env:"GIGACHAT_API_URL" envDefault:"https://gigachat.devices.sberbank.ru/api/v1"`
	OAuthURL     string `env:"GIGACHAT_OAUTH_URL" envDefault:"https://ngw.devices.sberbank.ru:9443/api/v2/oauth"`
	Model        string `env:"GIGACHAT_MODEL" envDefault:"GigaChat"`
}

type OAuth struct {
	GoogleClientID        string `env:"OAUTH_GOOGLE_CLIENT_ID"`
	GoogleClientSecret    string `env:"OAUTH_GOOGLE_CLIENT_SECRET"`
	GoogleRedirectURL     string `env:"OAUTH_GOOGLE_REDIRECT_URL"`
	MicrosoftClientID     string `env:"OAUTH_MICROSOFT_CLIENT_ID"`
	MicrosoftClientSecret string `env:"OAUTH_MICROSOFT_CLIENT_SECRET"`
	MicrosoftTenant       string `env:"OAUTH_MICROSOFT_TENANT" envDefault:"common"`
	MicrosoftRedirectURL  string `env:"OAUTH_MICROSOFT_REDIRECT_URL"`
	YandexClientID        string `env:"OAUTH_YANDEX_CLIENT_ID"`
	YandexClientSecret    string `env:"OAUTH_YANDEX_CLIENT_SECRET"`
	YandexRedirectURL     string `env:"OAUTH_YANDEX_REDIRECT_URL"`
}

type Risk struct {
	W1             float64 `env:"RISK_W1" envDefault:"0.30"`
	W2             float64 `env:"RISK_W2" envDefault:"0.25"`
	W3             float64 `env:"RISK_W3" envDefault:"0.20"`
	W4             float64 `env:"RISK_W4" envDefault:"0.15"`
	W5             float64 `env:"RISK_W5" envDefault:"0.10"`
	FreshnessDDays int     `env:"FRESHNESS_D_DAYS" envDefault:"90"`
}

type Asynq struct {
	Concurrency    int `env:"ASYNQ_CONCURRENCY" envDefault:"10"`
	QueuesDefault  int `env:"ASYNQ_QUEUES_DEFAULT" envDefault:"6"`
	QueuesCritical int `env:"ASYNQ_QUEUES_CRITICAL" envDefault:"3"`
	QueuesLow      int `env:"ASYNQ_QUEUES_LOW" envDefault:"1"`
}

type CORS struct {
	AllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:5173,http://localhost:3000"`
}

func Load() (*Config, error) {
	loadDotEnv()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadDotEnv() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	dir := cwd
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			_ = godotenv.Load(candidate)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func (c *Config) validate() error {
	if c.App.EncryptionKey == "" {
		return fmt.Errorf("APP_ENCRYPTION_KEY is required (base64, 32 bytes)")
	}
	total := c.Risk.W1 + c.Risk.W2 + c.Risk.W3 + c.Risk.W4 + c.Risk.W5
	if total < 0.99 || total > 1.01 {
		return fmt.Errorf("risk weights must sum to 1.0, got %.3f", total)
	}
	return nil
}

func (c *Config) IsProduction() bool {
	return c.App.Env == "production"
}
