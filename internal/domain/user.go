// Package domain holds entities and business rules. It performs no IO.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserRole is a capability flag carried by a user. A user may hold several.
type UserRole string

const (
	UserRoleSponsor  UserRole = "SPONSOR"
	UserRoleEngineer UserRole = "ENGINEER"
	UserRoleTechLead UserRole = "TECH_LEAD"
	UserRoleSteward  UserRole = "STEWARD"
)

// PlatformRole controls application-wide access independently from legacy
// capability roles.
type PlatformRole string

const (
	PlatformRoleAdmin  PlatformRole = "ADMIN"
	PlatformRoleMember PlatformRole = "MEMBER"
)

// User is a member of the team.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        *string
	AvatarURL    *string
	PlatformRole PlatformRole
	Roles        []UserRole
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

// UserRef is a light reference to a user embedded in another entity's API
// response — a task's assignee or creator. Embedding this (rather than just
// an ID) is what lets a task list response satisfy a frontend in a single
// round trip: it never has to look up who a task belongs to separately.
type UserRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email *string   `json:"email"`
}
