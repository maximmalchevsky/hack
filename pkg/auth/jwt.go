package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenType — назначение токена. Access короткоживущий, Refresh длинный.
type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

// Claims — JWT-полезная нагрузка.
type Claims struct {
	UserID     uuid.UUID `json:"uid"`
	Role       string    `json:"role"`
	EmployeeID uuid.UUID `json:"eid,omitempty"`
	TokenType  TokenType `json:"typ"`
	jwt.RegisteredClaims
}

// Manager — генерация и валидация JWT.
type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "worktimesync",
	}
}

// Issue — создаёт access + refresh токены для пользователя.
func (m *Manager) Issue(userID, employeeID uuid.UUID, role string) (access, refresh string, err error) {
	now := time.Now()

	access, err = m.sign(Claims{
		UserID:     userID,
		EmployeeID: employeeID,
		Role:       role,
		TokenType:  TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			ID:        uuid.NewString(),
		},
	})
	if err != nil {
		return "", "", err
	}

	refresh, err = m.sign(Claims{
		UserID:     userID,
		EmployeeID: employeeID,
		Role:       role,
		TokenType:  TokenRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
			ID:        uuid.NewString(),
		},
	})
	return access, refresh, err
}

func (m *Manager) sign(c Claims) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString(m.secret)
}

// Parse — валидирует и возвращает claims, либо ошибку.
func (m *Manager) Parse(tokenString string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// ParseAccess — то же что Parse, но проверяет что это именно access-токен.
func (m *Manager) ParseAccess(tokenString string) (*Claims, error) {
	c, err := m.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	if c.TokenType != TokenAccess {
		return nil, errors.New("not an access token")
	}
	return c, nil
}

// ParseRefresh — то же, но для refresh.
func (m *Manager) ParseRefresh(tokenString string) (*Claims, error) {
	c, err := m.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	if c.TokenType != TokenRefresh {
		return nil, errors.New("not a refresh token")
	}
	return c, nil
}
