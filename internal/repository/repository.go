// Package repository — слой доступа к данным.
//
// Каждый репозиторий принимает на вход *pgxpool.Pool и реализует
// набор методов CRUD для одной аггрегатной сущности. Бизнес-логика
// живёт в internal/service.
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound — стандартная ошибка "ничего не нашли".
var ErrNotFound = errors.New("repository: not found")

// AsNotFound — true если ошибка соответствует pgx.ErrNoRows.
func AsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrNotFound)
}
