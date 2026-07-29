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

// ErrForbidden means the caller is not allowed to perform this action on
// this entity — currently only comment edit/delete, which spec restricts to
// the comment's own author. This is ordinary ownership, not a workflow gate:
// the task rules explicitly relaxed require no reviewer or acceptance gate.
var ErrForbidden = errors.New("forbidden")

// ErrConflict means the request collides with an existing, unrelated
// constraint — currently only a duplicate label name. This is ordinary REST
// practice (a uniqueness violation), not a status-transition gate: the task
// rules relax every transition gate, not every database constraint.
var ErrConflict = errors.New("conflict")

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
