package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
	"worktimesync/pkg/auth"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrEmailTaken         = errors.New("auth: email already taken")
	ErrWeakPassword       = errors.New("auth: password must be at least 8 characters")
	ErrInvalidRole        = errors.New("auth: invalid role")
	ErrInvalidEmail       = errors.New("auth: invalid email")
)

type AuthService struct {
	pool  *pgxpool.Pool
	users *repository.UserRepo
	emps  *repository.EmployeeRepo
	jwt   *auth.Manager
}

func NewAuthService(pool *pgxpool.Pool, jwt *auth.Manager) *AuthService {
	return &AuthService{
		pool:  pool,
		users: repository.NewUserRepo(pool),
		emps:  repository.NewEmployeeRepo(pool),
		jwt:   jwt,
	}
}

type RegisterInput struct {
	Email    string
	Password string
	FullName string
	Role     domain.Role
	Timezone string
}

type TokenPair struct {
	Access  string
	Refresh string
}

type RegisterResult struct {
	Tokens   TokenPair
	User     domain.User
	Employee domain.Employee
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if !looksLikeEmail(in.Email) {
		return nil, ErrInvalidEmail
	}
	if len(in.Password) < 8 {
		return nil, ErrWeakPassword
	}
	if in.Role == "" {
		in.Role = domain.RoleEmployee
	}
	if !in.Role.Valid() {
		return nil, ErrInvalidRole
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, full_name, timezone)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, password_hash, role, full_name, timezone, locale,
		          COALESCE(avatar_url, ''), created_at, updated_at
	`, in.Email, hash, string(in.Role), in.FullName, defaultTZ(in.Timezone))

	user, err := scanUserRow(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	row2 := tx.QueryRow(ctx, `
		INSERT INTO employees (user_id)
		VALUES ($1)
		RETURNING id, user_id, COALESCE(department, ''), COALESCE(position, ''),
		          hr_work_format, hire_date, last_profile_update_at, last_confirmed_at,
		          manager_id, created_at, updated_at
	`, user.ID)

	emp, err := scanEmployeeRow(row2)
	if err != nil {
		return nil, fmt.Errorf("insert employee: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	access, refresh, err := s.jwt.Issue(user.ID, emp.ID, string(user.Role))
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	return &RegisterResult{
		Tokens:   TokenPair{Access: access, Refresh: refresh},
		User:     *user,
		Employee: *emp,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*RegisterResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !looksLikeEmail(email) || password == "" {
		return nil, ErrInvalidCredentials
	}

	user, err := s.users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !auth.VerifyPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	emp, err := s.emps.ByUserID(ctx, user.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	var empID uuid.UUID
	if emp != nil {
		empID = emp.ID
	}

	access, refresh, err := s.jwt.Issue(user.ID, empID, string(user.Role))
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}

	out := &RegisterResult{
		Tokens: TokenPair{Access: access, Refresh: refresh},
		User:   *user,
	}
	if emp != nil {
		out.Employee = *emp
	}
	return out, nil
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.jwt.ParseRefresh(refreshToken)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	user, err := s.users.ByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	emp, err := s.emps.ByUserID(ctx, user.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	var empID uuid.UUID
	if emp != nil {
		empID = emp.ID
	}

	access, refresh, err := s.jwt.Issue(user.ID, empID, string(user.Role))
	if err != nil {
		return nil, fmt.Errorf("issue tokens: %w", err)
	}
	return &TokenPair{Access: access, Refresh: refresh}, nil
}

func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-3 && strings.Contains(s[at+1:], ".")
}

func defaultTZ(tz string) string {
	if tz != "" {
		return tz
	}
	return "Europe/Moscow"
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key value") ||
		strings.Contains(err.Error(), "23505")
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUserRow(s rowScanner) (*domain.User, error) {
	var (
		u    domain.User
		role string
		now  time.Time
	)
	_ = now
	if err := s.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &role, &u.FullName,
		&u.Timezone, &u.Locale, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	u.Role = domain.Role(role)
	return &u, nil
}

func scanEmployeeRow(s rowScanner) (*domain.Employee, error) {
	var (
		emp    domain.Employee
		format *string
	)
	if err := s.Scan(
		&emp.ID, &emp.UserID, &emp.Department, &emp.Position,
		&format, &emp.HireDate, &emp.LastProfileUpdateAt, &emp.LastConfirmedAt,
		&emp.ManagerID, &emp.CreatedAt, &emp.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if format != nil {
		wf := domain.WorkFormat(*format)
		emp.HRWorkFormat = &wf
	}
	return &emp, nil
}
