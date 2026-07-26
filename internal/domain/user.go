// Package domain holds entities and business rules. It performs no IO.
package domain

import "github.com/google/uuid"

// UserRole is a capability flag carried by a user. A user may hold several.
type UserRole string

const (
	UserRoleSponsor  UserRole = "SPONSOR"
	UserRoleEngineer UserRole = "ENGINEER"
	UserRoleTechLead UserRole = "TECH_LEAD"
	UserRoleSteward  UserRole = "STEWARD"
)

// User is a member of the team.
type User struct {
	ID     uuid.UUID  `json:"id"`
	Name   string     `json:"name"`
	Email  string     `json:"email"`
	Roles  []UserRole `json:"roles"`
	Active bool       `json:"active"`
}

// HasRole reports whether the user carries the given role.
func (u User) HasRole(r UserRole) bool {
	for _, held := range u.Roles {
		if held == r {
			return true
		}
	}
	return false
}
