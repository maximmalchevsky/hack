// Package migrator — встраивает SQL-миграции в бинарник через embed.FS
// и накатывает их при старте процесса (cmd/api). На сервер не нужно тащить
// папку migrations/ — она уже внутри образа.
package migrator

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq" // драйвер для database/sql
	"github.com/rs/zerolog"
)

//go:embed migrations/*.sql
var embeddedFS embed.FS

// Up — накатывает все непримененные миграции. Идемпотентна.
func Up(pool *pgxpool.Pool, log zerolog.Logger) error {
	sub, err := fs.Sub(embeddedFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrator: fs.Sub: %w", err)
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		return fmt.Errorf("migrator: iofs.New: %w", err)
	}

	// pgx-pool не отдаёт *sql.DB, поэтому открываем отдельное короткоживущее
	// соединение по DSN (pgxpool сам параметры запомнил). Закроем после Up.
	cfg := pool.Config().ConnConfig
	sslmode := "disable"
	if v, ok := cfg.RuntimeParams["sslmode"]; ok && v != "" {
		sslmode = v
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, sslmode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("migrator: open sql: %w", err)
	}
	defer db.Close()

	drv, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migrator: pg driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", drv)
	if err != nil {
		return fmt.Errorf("migrator: new instance: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("migrations: no changes")
			return nil
		}
		return fmt.Errorf("migrator: up: %w", err)
	}

	if v, dirty, vErr := m.Version(); vErr == nil {
		log.Info().Uint("version", uint(v)).Bool("dirty", dirty).Msg("migrations: applied")
	}
	return nil
}
