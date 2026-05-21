package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/domain"
	"worktimesync/internal/service"
)

// AuthHandler — Fiber-обработчики для /api/v1/auth.
type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register регистрирует роуты в группе.
func (h *AuthHandler) Register(r fiber.Router) {
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
}

func (h *AuthHandler) register(c fiber.Ctx) error {
	var req RegisterRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	res, err := h.svc.Register(c.Context(), service.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
		Role:     domain.Role(req.Role),
		Timezone: req.Timezone,
	})
	if err != nil {
		return mapServiceErr(err)
	}

	emp := EmployeeToDTO(res.Employee)
	return c.Status(fiber.StatusCreated).JSON(AuthResponse{
		Tokens:   TokenPairResponse{Access: res.Tokens.Access, Refresh: res.Tokens.Refresh},
		User:     UserToDTO(res.User),
		Employee: &emp,
	})
}

func (h *AuthHandler) login(c fiber.Ctx) error {
	var req LoginRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	res, err := h.svc.Login(c.Context(), req.Email, req.Password)
	if err != nil {
		return mapServiceErr(err)
	}

	resp := AuthResponse{
		Tokens: TokenPairResponse{Access: res.Tokens.Access, Refresh: res.Tokens.Refresh},
		User:   UserToDTO(res.User),
	}
	if res.Employee.ID != [16]byte{} {
		emp := EmployeeToDTO(res.Employee)
		resp.Employee = &emp
	}
	return c.JSON(resp)
}

func (h *AuthHandler) refresh(c fiber.Ctx) error {
	var req RefreshRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	pair, err := h.svc.Refresh(c.Context(), req.Refresh)
	if err != nil {
		return mapServiceErr(err)
	}
	return c.JSON(TokenPairResponse{Access: pair.Access, Refresh: pair.Refresh})
}

// mapServiceErr — переводит доменные ошибки в HTTP-коды.
func mapServiceErr(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, service.ErrEmailTaken):
		return fiber.NewError(fiber.StatusConflict, "email already taken")
	case errors.Is(err, service.ErrWeakPassword):
		return fiber.NewError(fiber.StatusBadRequest, "password must be at least 8 characters")
	case errors.Is(err, service.ErrInvalidEmail):
		return fiber.NewError(fiber.StatusBadRequest, "invalid email")
	case errors.Is(err, service.ErrInvalidRole):
		return fiber.NewError(fiber.StatusBadRequest, "invalid role")
	default:
		return err
	}
}
