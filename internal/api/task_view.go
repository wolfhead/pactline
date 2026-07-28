package api

import (
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

// taskView is the JSON shape of a task, with its creator, assignee and
// labels already embedded — the exact information a list or detail view
// needs, in the one query that produced it, never a second round trip.
type taskView struct {
	ID          uuid.UUID           `json:"id"`
	Number      int64               `json:"number"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Status      domain.TaskStatus   `json:"status"`
	Priority    domain.TaskPriority `json:"priority"`
	Assignee    *domain.UserRef     `json:"assignee"`
	Creator     domain.UserRef      `json:"creator"`
	DueDate     *string             `json:"due_date"`
	Project     *taskProjectView    `json:"project"`
	Milestone   *taskMilestoneView  `json:"milestone"`
	Labels      []domain.Label      `json:"labels"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	CompletedAt *time.Time          `json:"completed_at"`
	ArchivedAt  *time.Time          `json:"archived_at"`
}

func newTaskView(twr store.TaskWithRelations) taskView {
	var due *string
	if twr.Task.DueDate != nil {
		s := twr.Task.DueDate.Format("2006-01-02")
		due = &s
	}
	labels := twr.Labels
	if labels == nil {
		labels = []domain.Label{}
	}
	var project *taskProjectView
	if twr.Project != nil {
		project = &taskProjectView{ID: twr.Project.ID, Number: twr.Project.Number, Name: twr.Project.Name}
	}
	var milestone *taskMilestoneView
	if twr.Milestone != nil {
		milestone = &taskMilestoneView{ID: twr.Milestone.ID, Name: twr.Milestone.Name}
	}
	return taskView{
		ID:          twr.Task.ID,
		Number:      twr.Task.Number,
		Title:       twr.Task.Title,
		Description: twr.Task.Description,
		Status:      twr.Task.Status,
		Priority:    twr.Task.Priority,
		Assignee:    twr.Assignee,
		Creator:     twr.Creator,
		DueDate:     due,
		Project:     project,
		Milestone:   milestone,
		Labels:      labels,
		CreatedAt:   twr.Task.CreatedAt,
		UpdatedAt:   twr.Task.UpdatedAt,
		CompletedAt: twr.Task.CompletedAt,
		ArchivedAt:  twr.Task.ArchivedAt,
	}
}

type taskProjectView struct {
	ID     uuid.UUID `json:"id"`
	Number int64     `json:"number"`
	Name   string    `json:"name"`
}

type taskMilestoneView struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// taskListResponse is the envelope GET /api/tasks returns: a page of tasks
// plus enough to fetch the next page. NextCursor is empty exactly when
// HasMore is false.
type taskListResponse struct {
	Items      []taskView `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

// commentView is the JSON shape of a comment.
type commentView struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	AuthorID  uuid.UUID `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newCommentView(c domain.Comment) commentView {
	return commentView{
		ID: c.ID, TaskID: c.TaskID, AuthorID: c.AuthorID, Body: c.Body,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// activityView is the JSON shape of one activity log entry.
type activityView struct {
	ID        uuid.UUID `json:"id"`
	ActorID   uuid.UUID `json:"actor_id"`
	Field     string    `json:"field"`
	OldValue  *string   `json:"old_value"`
	NewValue  *string   `json:"new_value"`
	CreatedAt time.Time `json:"created_at"`
}

func newActivityView(a domain.Activity) activityView {
	return activityView{
		ID: a.ID, ActorID: a.ActorID, Field: string(a.Field),
		OldValue: a.OldValue, NewValue: a.NewValue, CreatedAt: a.CreatedAt,
	}
}
