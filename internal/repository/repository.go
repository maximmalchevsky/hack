package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("repository: not found")

func AsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrNotFound)
}
