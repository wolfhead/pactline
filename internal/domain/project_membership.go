package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ProjectRole is a Project-local authorization role.
type ProjectRole string

const (
	ProjectRoleAdmin  ProjectRole = "admin"
	ProjectRoleMember ProjectRole = "member"
)

func (r ProjectRole) Valid() bool {
	return r == ProjectRoleAdmin || r == ProjectRoleMember
}

type ProjectMembership struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	User      UserRef
	Role      ProjectRole
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (m ProjectMembership) Validate() error {
	if m.ID == uuid.Nil || m.ProjectID == uuid.Nil || m.User.ID == uuid.Nil || !m.Role.Valid() {
		return fmt.Errorf("%w: valid project, user, and role are required", ErrInvalidInput)
	}
	return nil
}

// ProjectAccessSubject is the effective identity used by Project authorization.
// Platform Administrator access is intentionally independent from Project role.
type ProjectAccessSubject struct {
	UserID       uuid.UUID
	PlatformRole PlatformRole
}

func (s ProjectAccessSubject) IsPlatformAdministrator() bool {
	return s.PlatformRole == PlatformRoleAdmin
}
