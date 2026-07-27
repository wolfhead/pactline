package domain

import "errors"

// ErrNotFound is returned when a requested entity does not exist. It is
// shared across every store (user, and the legacy mechanism stores under
// internal/legacy/store) because "not found" carries the same meaning
// everywhere: the mapping to HTTP 404 in internal/api/response.go applies to
// it regardless of which entity raised it.
var ErrNotFound = errors.New("not found")
