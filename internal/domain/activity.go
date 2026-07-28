package domain

import (
	"time"

	"github.com/google/uuid"
)

// ActivityField names which attribute an Activity entry records a change to.
// It doubles as the entry's "kind": e.g. a row with Field ==
// ActivityFieldStatus records a status move.
type ActivityField string

const (
	ActivityFieldCreated     ActivityField = "created"
	ActivityFieldTitle       ActivityField = "title"
	ActivityFieldDescription ActivityField = "description"
	ActivityFieldStatus      ActivityField = "status"
	ActivityFieldPriority    ActivityField = "priority"
	ActivityFieldAssignee    ActivityField = "assignee"
	ActivityFieldDueDate     ActivityField = "due_date"
	ActivityFieldLabels      ActivityField = "labels"
	ActivityFieldProject     ActivityField = "project"
	ActivityFieldMilestone   ActivityField = "milestone"
	ActivityFieldArchived    ActivityField = "archived"
)

// Activity is one append-only record of a change made to a task: what
// changed, the value before and after (as display-ready text; nil means
// "none"/"unset"), who made it and when. It is written by the same store
// code path that performs the underlying change (see task_store.go), so it
// cannot drift from what actually happened.
type Activity struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	ActorID   uuid.UUID
	Field     ActivityField
	OldValue  *string
	NewValue  *string
	CreatedAt time.Time
}
