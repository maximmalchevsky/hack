// Package locks — distributed lock через Redis SETNX + TTL.
//
// Используется для предотвращения параллельного запуска тяжёлых задач
// (например, recompute метрик для одного employee или sync одной интеграции),
// когда несколько worker'ов в кластере могут одновременно поднять одинаковый job.
package locks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLocked — лок уже захвачен другим процессом.
var ErrLocked = errors.New("locks: already locked")

// Manager — обёртка над Redis для управления локами.
type Manager struct {
	rdb *redis.Client
}

func NewManager(rdb *redis.Client) *Manager { return &Manager{rdb: rdb} }

// Lock — попытка захватить лок. Возвращает release-функцию или ErrLocked.
//
// release безопасно вызывать несколько раз: при повторных вызовах будет no-op.
// release использует Lua-скрипт для атомарного "удалить если значение совпадает",
// чтобы случайно не снять чужой лок, если наш TTL истёк.
func (m *Manager) Lock(ctx context.Context, key string, ttl time.Duration) (release func(), err error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	ok, err := m.rdb.SetNX(ctx, fullKey(key), token, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLocked
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		_, _ = m.rdb.Eval(ctx, `
			if redis.call('GET', KEYS[1]) == ARGV[1] then
				return redis.call('DEL', KEYS[1])
			end
			return 0
		`, []string{fullKey(key)}, token).Result()
	}, nil
}

// TryLockOrSkip — обёртка для типичного use-case: "если не залочено — выполнить fn, иначе пропустить".
// Возвращает true если fn был выполнен, false если был skip из-за активного лока.
func (m *Manager) TryLockOrSkip(ctx context.Context, key string, ttl time.Duration, fn func(ctx context.Context) error) (bool, error) {
	release, err := m.Lock(ctx, key, ttl)
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return false, nil
		}
		return false, err
	}
	defer release()
	if err := fn(ctx); err != nil {
		return true, err
	}
	return true, nil
}

func fullKey(k string) string { return "wts:lock:" + k }

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
