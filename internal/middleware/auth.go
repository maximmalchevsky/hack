// Package middleware — Fiber middleware'ы для авторизации и RBAC.
package middleware

import (
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/pkg/auth"
)

// Контекстные ключи для извлечения данных авторизованного пользователя
// в обработчиках через c.Locals(...).
const (
	ctxKeyUserID     = "wts:user_id"
	ctxKeyEmployeeID = "wts:employee_id"
	ctxKeyRole       = "wts:role"
)

// AuthRequired — проверяет валидный access JWT.
// Сначала смотрит заголовок Authorization: Bearer <token>, затем (для SSE/EventSource,
// которые не умеют слать кастомные заголовки) — query-параметр ?token=.
func AuthRequired(jwt *auth.Manager) fiber.Handler {
	return func(c fiber.Ctx) error {
		token := extractToken(c)
		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing access token")
		}
		claims, err := jwt.ParseAccess(token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}

		c.Locals(ctxKeyUserID, claims.UserID)
		c.Locals(ctxKeyEmployeeID, claims.EmployeeID)
		c.Locals(ctxKeyRole, domain.Role(claims.Role))
		return c.Next()
	}
}

func extractToken(c fiber.Ctx) string {
	if h := c.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}
	return c.Query("token")
}

// RequireRole — пропускает только пользователей с указанной ролью (любой из списка).
// Должен ставиться после AuthRequired.
func RequireRole(roles ...domain.Role) fiber.Handler {
	return func(c fiber.Ctx) error {
		current, ok := c.Locals(ctxKeyRole).(domain.Role)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "missing role in context")
		}
		if !slices.Contains(roles, current) {
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		}
		return c.Next()
	}
}

// --- хелперы для извлечения значений из контекста ---

// UserID — текущий user ID (или zero, если не задан).
func UserID(c fiber.Ctx) uuid.UUID {
	v, _ := c.Locals(ctxKeyUserID).(uuid.UUID)
	return v
}

// EmployeeID — текущий employee ID.
func EmployeeID(c fiber.Ctx) uuid.UUID {
	v, _ := c.Locals(ctxKeyEmployeeID).(uuid.UUID)
	return v
}

// CurrentRole — роль текущего пользователя.
func CurrentRole(c fiber.Ctx) domain.Role {
	v, _ := c.Locals(ctxKeyRole).(domain.Role)
	return v
}
