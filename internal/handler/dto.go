// Package handler — Fiber-обработчики HTTP и DTO для входа/выхода.
package handler

import (
	"time"

	"github.com/google/uuid"

	"worktimesync/internal/domain"
)

// --- Auth DTO ---

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Role     string `json:"role,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	Refresh string `json:"refresh"`
}

type TokenPairResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

type AuthResponse struct {
	Tokens   TokenPairResponse `json:"tokens"`
	User     UserDTO           `json:"user"`
	Employee *EmployeeDTO      `json:"employee,omitempty"`
}

// UserDTO — публичная форма пользователя (без password_hash).
type UserDTO struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	FullName  string    `json:"full_name"`
	Timezone  string    `json:"timezone"`
	Locale    string    `json:"locale"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func UserToDTO(u domain.User) UserDTO {
	return UserDTO{
		ID:        u.ID,
		Email:     u.Email,
		Role:      string(u.Role),
		FullName:  u.FullName,
		Timezone:  u.Timezone,
		Locale:    u.Locale,
		AvatarURL: u.AvatarURL,
		CreatedAt: u.CreatedAt,
	}
}

// EmployeeDTO — публичная форма employee.
type EmployeeDTO struct {
	ID                  uuid.UUID  `json:"id"`
	UserID              uuid.UUID  `json:"user_id"`
	Department          string     `json:"department,omitempty"`
	Position            string     `json:"position,omitempty"`
	HRWorkFormat        string     `json:"hr_work_format,omitempty"`
	HireDate            *time.Time `json:"hire_date,omitempty"`
	LastProfileUpdateAt *time.Time `json:"last_profile_update_at,omitempty"`
	LastConfirmedAt     *time.Time `json:"last_confirmed_at,omitempty"`
	ManagerID           *uuid.UUID `json:"manager_id,omitempty"`
}

func EmployeeToDTO(e domain.Employee) EmployeeDTO {
	dto := EmployeeDTO{
		ID:                  e.ID,
		UserID:              e.UserID,
		Department:          e.Department,
		Position:            e.Position,
		HireDate:            e.HireDate,
		LastProfileUpdateAt: e.LastProfileUpdateAt,
		LastConfirmedAt:     e.LastConfirmedAt,
		ManagerID:           e.ManagerID,
	}
	if e.HRWorkFormat != nil {
		dto.HRWorkFormat = string(*e.HRWorkFormat)
	}
	return dto
}

// ErrorResponse — стандартная форма ошибки.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// --- Profile DTO ---

type DayHoursDTO struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type DaysOfWeekDTO struct {
	Mon *DayHoursDTO `json:"mon,omitempty"`
	Tue *DayHoursDTO `json:"tue,omitempty"`
	Wed *DayHoursDTO `json:"wed,omitempty"`
	Thu *DayHoursDTO `json:"thu,omitempty"`
	Fri *DayHoursDTO `json:"fri,omitempty"`
	Sat *DayHoursDTO `json:"sat,omitempty"`
	Sun *DayHoursDTO `json:"sun,omitempty"`
}

type WorkProfileDTO struct {
	ID         uuid.UUID     `json:"id"`
	EmployeeID uuid.UUID     `json:"employee_id"`
	ValidFrom  time.Time     `json:"valid_from"`
	ValidTo    *time.Time    `json:"valid_to,omitempty"`
	DaysOfWeek DaysOfWeekDTO `json:"days_of_week"`
	Timezone   string        `json:"timezone"`
	WorkFormat string        `json:"work_format"`
	Source     string        `json:"source"`
	IsActive   bool          `json:"is_active"`
	CreatedAt  time.Time     `json:"created_at"`
}

func ProfileToDTO(p domain.WorkProfile) WorkProfileDTO {
	return WorkProfileDTO{
		ID:         p.ID,
		EmployeeID: p.EmployeeID,
		ValidFrom:  p.ValidFrom,
		ValidTo:    p.ValidTo,
		DaysOfWeek: daysFromDomain(p.DaysOfWeek),
		Timezone:   p.Timezone,
		WorkFormat: string(p.WorkFormat),
		Source:     p.Source,
		IsActive:   p.IsActive(),
		CreatedAt:  p.CreatedAt,
	}
}

func daysFromDomain(d domain.DaysOfWeek) DaysOfWeekDTO {
	conv := func(h *domain.DayHours) *DayHoursDTO {
		if h == nil {
			return nil
		}
		return &DayHoursDTO{Start: h.Start, End: h.End}
	}
	return DaysOfWeekDTO{
		Mon: conv(d.Mon), Tue: conv(d.Tue), Wed: conv(d.Wed),
		Thu: conv(d.Thu), Fri: conv(d.Fri),
		Sat: conv(d.Sat), Sun: conv(d.Sun),
	}
}

func DaysToDomain(d DaysOfWeekDTO) domain.DaysOfWeek {
	conv := func(h *DayHoursDTO) *domain.DayHours {
		if h == nil {
			return nil
		}
		return &domain.DayHours{Start: h.Start, End: h.End}
	}
	return domain.DaysOfWeek{
		Mon: conv(d.Mon), Tue: conv(d.Tue), Wed: conv(d.Wed),
		Thu: conv(d.Thu), Fri: conv(d.Fri),
		Sat: conv(d.Sat), Sun: conv(d.Sun),
	}
}

type UpdateProfileRequest struct {
	DaysOfWeek DaysOfWeekDTO `json:"days_of_week"`
	Timezone   string        `json:"timezone"`
	WorkFormat string        `json:"work_format"`
}

// --- TimeException DTO ---

type TimeExceptionDTO struct {
	ID         uuid.UUID `json:"id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	Kind       string    `json:"kind"`
	StartAt    time.Time `json:"start_at"`
	EndAt      time.Time `json:"end_at"`
	Comment    string    `json:"comment,omitempty"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}

func ExceptionToDTO(e domain.TimeException) TimeExceptionDTO {
	return TimeExceptionDTO{
		ID:         e.ID,
		EmployeeID: e.EmployeeID,
		Kind:       string(e.Kind),
		StartAt:    e.StartAt,
		EndAt:      e.EndAt,
		Comment:    e.Comment,
		Source:     e.Source,
		CreatedAt:  e.CreatedAt,
	}
}

type CreateExceptionRequest struct {
	Kind    string    `json:"kind"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
	Comment string    `json:"comment,omitempty"`
}

// --- /me DTO (расширенная) ---

type MeResponse struct {
	User        UserDTO            `json:"user"`
	Employee    *EmployeeDTO       `json:"employee,omitempty"`
	WorkProfile *WorkProfileDTO    `json:"work_profile,omitempty"`
	Exceptions  []TimeExceptionDTO `json:"exceptions,omitempty"`
}

// --- Integration DTO ---

type IntegrationDTO struct {
	ID           uuid.UUID  `json:"id"`
	EmployeeID   uuid.UUID  `json:"employee_id"`
	Provider     string     `json:"provider"`
	AccountEmail string     `json:"account_email,omitempty"`
	AccountLabel string     `json:"account_label,omitempty"`
	Status       string     `json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func IntegrationToDTO(i domain.Integration) IntegrationDTO {
	return IntegrationDTO{
		ID:           i.ID,
		EmployeeID:   i.EmployeeID,
		Provider:     string(i.Provider),
		AccountEmail: i.AccountEmail,
		AccountLabel: i.AccountLabel,
		Status:       string(i.Status),
		LastSyncAt:   i.LastSyncAt,
		LastError:    i.LastError,
		CreatedAt:    i.CreatedAt,
	}
}

type ConnectICalRequest struct {
	FeedURL string `json:"feed_url,omitempty"`
	Label   string `json:"label,omitempty"`
}

type ConnectCalDAVRequest struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
	CalPath  string `json:"cal_path,omitempty"`
	Label    string `json:"label,omitempty"`
}

type ConnectJiraRequest struct {
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
	Label    string `json:"label,omitempty"`
}

// --- Tracker tasks DTO ---

type TaskSlotDTO struct {
	Date  string  `json:"date"` // YYYY-MM-DD
	Hours float64 `json:"hours"`
}

type TrackerTaskDTO struct {
	ID               uuid.UUID  `json:"id"`
	IntegrationID    *uuid.UUID `json:"integration_id,omitempty"`
	SourceTaskID     string     `json:"source_task_id"`
	Title            string     `json:"title"`
	Description      string     `json:"description,omitempty"`
	Status           string     `json:"status,omitempty"`
	Priority         string     `json:"priority,omitempty"`
	Type             string     `json:"type,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	EstimatedHours   *float64   `json:"estimated_hours,omitempty"`
	ActualHours      *float64   `json:"actual_hours,omitempty"`
	AIEstimatedHours *float64   `json:"ai_estimated_hours,omitempty"`
	AIConfidence     *float64   `json:"ai_confidence,omitempty"`
	Slots            []TaskSlotDTO `json:"slots,omitempty"`
}

func TrackerTaskToDTO(t domain.TrackerTask) TrackerTaskDTO {
	return TrackerTaskDTO{
		ID:               t.ID,
		IntegrationID:    t.IntegrationID,
		SourceTaskID:     t.SourceTaskID,
		Title:            t.Title,
		Description:      t.Description,
		Status:           t.Status,
		Priority:         string(t.Priority),
		Type:             t.Type,
		DueAt:            t.DueAt,
		EstimatedHours:   t.EstimatedHours,
		ActualHours:      t.ActualHours,
		AIEstimatedHours: t.AIEstimatedHours,
		AIConfidence:     t.AIConfidence,
	}
}

// --- Calendar Event DTO ---

type CalendarEventDTO struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description,omitempty"`
	StartAt        time.Time  `json:"start_at"`
	EndAt          time.Time  `json:"end_at"`
	Timezone       string     `json:"timezone,omitempty"`
	AttendeesCount int        `json:"attendees_count,omitempty"`
	Organizer      string     `json:"organizer,omitempty"`
	Status         string     `json:"status"`
	IsExcluded     bool       `json:"is_excluded,omitempty"`
	Category       string     `json:"category,omitempty"`
	// IntegrationID — UUID интеграции, через которую событие синкнуто. Null
	// означает «нативное» событие Workie (создано через /scheduler или seed).
	// На фронте — ключ фильтра «Источники» в стиле Outlook.
	IntegrationID *uuid.UUID `json:"integration_id,omitempty"`
}

func EventToDTO(e domain.CalendarEvent) CalendarEventDTO {
	cat := ""
	if e.Category != nil {
		cat = *e.Category
	}
	return CalendarEventDTO{
		ID:             e.ID,
		Title:          e.Title,
		Description:    e.Description,
		StartAt:        e.StartAt,
		EndAt:          e.EndAt,
		Timezone:       e.Timezone,
		AttendeesCount: e.AttendeesCount,
		Organizer:      e.Organizer,
		Status:         string(e.Status),
		IsExcluded:     e.IsExcluded,
		Category:       cat,
		IntegrationID:  e.IntegrationID,
	}
}

// --- Recommendation DTO ---

type RecommendationDTO struct {
	ID          uuid.UUID      `json:"id"`
	EmployeeID  *uuid.UUID     `json:"employee_id,omitempty"`
	Employee    *EmployeeRefDTO `json:"employee,omitempty"`
	Kind        string         `json:"kind"`
	Priority    string         `json:"priority"`
	Title       string         `json:"title"`
	Explanation string         `json:"explanation"`
	Status      string         `json:"status"`
	GeneratedBy string         `json:"generated_by"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// EmployeeRefDTO — компактная ссылка на сотрудника в ответах,
// возвращающих рекомендации/диагностику команды или всей компании.
type EmployeeRefDTO struct {
	ID         uuid.UUID `json:"id"`
	FullName   string    `json:"full_name"`
	Role       string    `json:"role,omitempty"`
	Department string    `json:"department,omitempty"`
}

// --- AI Chat DTO ---

type AIChatRequest struct {
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	Message        string     `json:"message"`
}

type AIChatResponse struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Answer         string    `json:"answer"`
	Available      bool      `json:"available"`
}
