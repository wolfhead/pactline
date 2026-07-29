package domain

import (
	"time"

	"github.com/google/uuid"
)

// Label is a named, reusable, renameable tag on tasks. It is a real table
// (not a string array on the task) precisely so that renaming one is an
// ordinary, cheap operation that updates every task wearing it at once.
type Label struct {
	ID        uuid.UUID
	Name      string
	Version   int64
	CreatedAt time.Time
}
