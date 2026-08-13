package domain

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a requested entity does not exist. It is
// shared across every store (user, and the legacy mechanism stores under
// internal/legacy/store) because "not found" carries the same meaning
// everywhere: the mapping to HTTP 404 in internal/api/response.go applies to
// it regardless of which entity raised it.
var ErrNotFound = errors.New("not found")

// ErrInvalidInput means the caller's request is malformed in a way no field
// value can fix by itself: an unknown status or priority string, a blank
// title, a blank label name. Distinct from ErrNotFound (the referenced
// entity doesn't exist) and ErrForbidden (the actor isn't allowed to act).
var ErrInvalidInput = errors.New("invalid input")

// ErrForbidden means the authenticated actor is not allowed to perform an
// operation on the requested entity or does not own the referenced Claim or
// editable Thread Item.
var ErrForbidden = errors.New("forbidden")

// ErrConflict means the request conflicts with current durable state.
var ErrConflict = errors.New("conflict")

// Workflow conflict categories remain compatible with ErrConflict while
// giving transports stable machine-readable rejection codes.
var (
	ErrInvalidTransition = fmt.Errorf("%w: invalid Task transition", ErrConflict)
	ErrActiveClaim       = fmt.Errorf("%w: Task already has an active Claim", ErrConflict)
	ErrActiveIssue       = fmt.Errorf("%w: Task already has an active Issue", ErrConflict)
	ErrMigrationRequired = fmt.Errorf("%w: Task lifecycle migration is required", ErrConflict)
	ErrWrongIssueType    = fmt.Errorf("%w: unsupported Issue type", ErrInvalidInput)
)

// ErrVersionConflict identifies an optimistic-concurrency failure.
var ErrVersionConflict = errors.New("version conflict")

// VersionConflictError carries the current persisted version without exposing
// storage details to transport adapters.
type VersionConflictError struct {
	CurrentVersion int64
}

func (e VersionConflictError) Error() string {
	return fmt.Sprintf("%s: current version is %d", ErrVersionConflict, e.CurrentVersion)
}

func (e VersionConflictError) Unwrap() error {
	return ErrVersionConflict
}
