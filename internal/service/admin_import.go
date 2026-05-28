package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/pkg/auth"
)

type AdminImportService struct {
	pool *pgxpool.Pool
}

func NewAdminImportService(pool *pgxpool.Pool) *AdminImportService {
	return &AdminImportService{pool: pool}
}

type CreatedRow struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Password string `json:"password"`
}

type SkippedRow struct {
	Row    int    `json:"row"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type ErrorRow struct {
	Row int    `json:"row"`
	Msg string `json:"msg"`
}

type ImportResult struct {
	Created []CreatedRow `json:"created"`
	Skipped []SkippedRow `json:"skipped"`
	Errors  []ErrorRow   `json:"errors"`
}

var supportedCols = map[string]bool{
	"email":         true,
	"full_name":     true,
	"department":    true,
	"position":      true,
	"timezone":      true,
	"hire_date":     true,
	"manager_email": true,
}

func (s *AdminImportService) Import(ctx context.Context, r io.Reader) (ImportResult, error) {
	res := ImportResult{Created: []CreatedRow{}, Skipped: []SkippedRow{}, Errors: []ErrorRow{}}

	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return res, fmt.Errorf("csv header: %w", err)
	}
	colIdx := map[string]int{}
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		if supportedCols[key] {
			colIdx[key] = i
		}
	}
	if _, ok := colIdx["email"]; !ok {
		return res, errors.New("csv: missing required column 'email'")
	}
	if _, ok := colIdx["full_name"]; !ok {
		return res, errors.New("csv: missing required column 'full_name'")
	}

	get := func(row []string, key string) string {
		i, ok := colIdx[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	rowNum := 1
	for {
		rowNum++
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: err.Error()})
			continue
		}

		email := strings.ToLower(get(row, "email"))
		fullName := get(row, "full_name")
		if email == "" || fullName == "" {
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "email и full_name обязательны"})
			continue
		}

		var existsID uuid.UUID
		err = s.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&existsID)
		if err == nil {
			res.Skipped = append(res.Skipped, SkippedRow{Row: rowNum, Email: email, Reason: "email уже существует"})
			continue
		}

		password := genPassword(12)
		hash, err := auth.HashPassword(password)
		if err != nil {
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "hash: " + err.Error()})
			continue
		}
		tz := get(row, "timezone")
		if tz == "" {
			tz = "Europe/Moscow"
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "tx begin: " + err.Error()})
			continue
		}

		var userID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role, timezone, locale)
			VALUES ($1, $2, $3, $4, $5, 'ru')
			RETURNING id
		`, email, hash, fullName, domain.RoleEmployee, tz).Scan(&userID)
		if err != nil {
			tx.Rollback(ctx)
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "users: " + err.Error()})
			continue
		}

		var managerEmpID *uuid.UUID
		if me := strings.ToLower(get(row, "manager_email")); me != "" {
			var mid uuid.UUID
			if err := tx.QueryRow(ctx, `
				SELECT e.id FROM employees e
				JOIN users u ON u.id = e.user_id
				WHERE u.email = $1
			`, me).Scan(&mid); err == nil {
				managerEmpID = &mid
			}
		}

		var hireDate *time.Time
		if hd := get(row, "hire_date"); hd != "" {
			for _, layout := range []string{"2006-01-02", "02.01.2006"} {
				if t, err := time.Parse(layout, hd); err == nil {
					hireDate = &t
					break
				}
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO employees (user_id, department, position, manager_id, hire_date)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5)
		`, userID, get(row, "department"), get(row, "position"), managerEmpID, hireDate)
		if err != nil {
			tx.Rollback(ctx)
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "employees: " + err.Error()})
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			res.Errors = append(res.Errors, ErrorRow{Row: rowNum, Msg: "commit: " + err.Error()})
			continue
		}

		res.Created = append(res.Created, CreatedRow{
			Email:    email,
			FullName: fullName,
			Password: password,
		})
	}

	return res, nil
}

func genPassword(byteLen int) string {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("Pass-%d", time.Now().UnixNano())
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}
