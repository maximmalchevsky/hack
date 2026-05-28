package domain

import "slices"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
	RoleManager  Role = "manager"
	RoleHR       Role = "hr"
	RolePM       Role = "pm"
	RoleAnalyst  Role = "analyst"
)

var AllRoles = []Role{
	RoleAdmin, RoleEmployee, RoleManager, RoleHR, RolePM, RoleAnalyst,
}

func (r Role) Valid() bool {
	return slices.Contains(AllRoles, r)
}

func (r Role) String() string { return string(r) }
