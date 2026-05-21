package domain

import "slices"

// Role — роль пользователя в системе.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
	RoleManager  Role = "manager"
	RoleHR       Role = "hr"
	RolePM       Role = "pm"
	RoleAnalyst  Role = "analyst"
)

// AllRoles — упорядоченный список всех ролей (для итерации и UI).
var AllRoles = []Role{
	RoleAdmin, RoleEmployee, RoleManager, RoleHR, RolePM, RoleAnalyst,
}

// Valid — true если роль входит в допустимое множество.
func (r Role) Valid() bool {
	return slices.Contains(AllRoles, r)
}

// String — для логов и JSON.
func (r Role) String() string { return string(r) }
