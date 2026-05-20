package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

func New(level string, pretty bool) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}

	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	return zerolog.New(out).
		Level(lvl).
		With().
		Timestamp().
		Str("service", "worktimesync").
		Logger()
}
