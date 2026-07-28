package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TaskStore reads and writes tasks, their labels and their activity log.
type TaskStore struct{ db *DB }

// NewTaskStore wires a TaskStore to the pool.
func NewTaskStore(db *DB) *TaskStore { return &TaskStore{db: db} }

const taskColumns = `t.id, t.number, t.title, t.description, t.status, t.priority,
	t.assignee_id, t.creator_id, t.due_date, t.project_id, t.milestone_id,
	t.created_at, t.updated_at, t.completed_at, t.archived_at`

// taskSelectColumns adds the joined creator/assignee/labels columns that
// let every task-reading query (Create, GetByNumber, List, Update,
// SetArchived) return a full TaskWithRelations in a single round trip: a
// list view never has to fetch a task's assignee or labels separately.
const taskSelectColumns = taskColumns + `,
	cu.id, cu.name, cu.email,
	au.id, au.name, au.email,
	p.id, p.number, p.name,
	m.id, m.name,
	COALESCE(lbl.labels, '[]'::json)`

const taskFromJoins = `FROM tasks t
	JOIN users cu ON cu.id = t.creator_id
	LEFT JOIN users au ON au.id = t.assignee_id
	LEFT JOIN projects p ON p.id = t.project_id
	LEFT JOIN milestones m ON m.id = t.milestone_id
	LEFT JOIN LATERAL (
		SELECT json_agg(json_build_object('id', l.id, 'name', l.name, 'created_at', l.created_at) ORDER BY l.name) AS labels
		FROM task_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.task_id = t.id
	) lbl ON true`

// TaskWithRelations is a task together with the entities a frontend needs
// alongside it in one response: the creator, the assignee (nil when
// unassigned — a normal, first-class state), and every label it wears.
type TaskWithRelations struct {
	Task      domain.Task
	Creator   domain.UserRef
	Assignee  *domain.UserRef
	Project   *ProjectRef
	Milestone *MilestoneRef
	Labels    []domain.Label
}

type ProjectRef struct {
	ID     uuid.UUID
	Number int64
	Name   string
}

type MilestoneRef struct {
	ID   uuid.UUID
	Name string
}

func scanTaskWithRelations(s scanner) (TaskWithRelations, error) {
	var (
		twr           TaskWithRelations
		assigneeID    *uuid.UUID
		assigneeName  *string
		assigneeEmail *string
		projectID     *uuid.UUID
		projectNumber *int64
		projectName   *string
		milestoneID   *uuid.UUID
		milestoneName *string
		labelsJSON    []byte
	)
	err := s.Scan(
		&twr.Task.ID, &twr.Task.Number, &twr.Task.Title, &twr.Task.Description, &twr.Task.Status, &twr.Task.Priority,
		&twr.Task.AssigneeID, &twr.Task.CreatorID, &twr.Task.DueDate, &twr.Task.ProjectID, &twr.Task.MilestoneID,
		&twr.Task.CreatedAt, &twr.Task.UpdatedAt,
		&twr.Task.CompletedAt, &twr.Task.ArchivedAt,
		&twr.Creator.ID, &twr.Creator.Name, &twr.Creator.Email,
		&assigneeID, &assigneeName, &assigneeEmail,
		&projectID, &projectNumber, &projectName,
		&milestoneID, &milestoneName,
		&labelsJSON,
	)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("scan task: %w", err)
	}
	if assigneeID != nil {
		twr.Assignee = &domain.UserRef{ID: *assigneeID, Name: derefStr(assigneeName), Email: assigneeEmail}
	}
	if projectID != nil {
		twr.Project = &ProjectRef{ID: *projectID, Number: *projectNumber, Name: derefStr(projectName)}
	}
	if milestoneID != nil {
		twr.Milestone = &MilestoneRef{ID: *milestoneID, Name: derefStr(milestoneName)}
	}
	labels := []domain.Label{}
	if err := json.Unmarshal(labelsJSON, &labels); err != nil {
		return TaskWithRelations{}, fmt.Errorf("unmarshal labels for task %s: %w", twr.Task.ID, err)
	}
	twr.Labels = labels
	return twr, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Create inserts a task, assigning an ID and a sequential Number, and
// records the creation in the activity log within the same transaction so
// the two can never drift apart. Status/Priority default to todo/none
// when left blank, mirroring the column defaults.
func (s *TaskStore) Create(ctx context.Context, t domain.Task, labelIDs []uuid.UUID) (TaskWithRelations, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if strings.TrimSpace(t.Title) == "" {
		return TaskWithRelations{}, fmt.Errorf("%w: title is required", domain.ErrInvalidInput)
	}
	if t.Status == "" {
		t.Status = domain.TaskStatusTodo
	}
	if t.Priority == "" {
		t.Priority = domain.TaskPriorityNone
	}
	if !t.Status.Valid() {
		return TaskWithRelations{}, fmt.Errorf("%w: unknown status %q", domain.ErrInvalidInput, t.Status)
	}
	if !t.Priority.Valid() {
		return TaskWithRelations{}, fmt.Errorf("%w: unknown priority %q", domain.ErrInvalidInput, t.Priority)
	}

	var completedAt *time.Time
	if t.Status == domain.TaskStatusDone {
		now := time.Now().UTC()
		completedAt = &now
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("begin create task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks
			(id, title, description, status, priority, assignee_id, creator_id,
			 due_date, project_id, milestone_id, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id`,
		t.ID, t.Title, t.Description, string(t.Status), string(t.Priority), t.AssigneeID,
		t.CreatorID, t.DueDate, t.ProjectID, t.MilestoneID, completedAt,
	).Scan(&id)
	if err != nil {
		return TaskWithRelations{}, mapPgError(err)
	}

	if len(labelIDs) > 0 {
		if err := replaceTaskLabels(ctx, tx, id, labelIDs); err != nil {
			return TaskWithRelations{}, err
		}
	}

	statusVal := string(t.Status)
	if err := insertActivity(ctx, tx, id, t.CreatorID, domain.ActivityFieldCreated, nil, &statusVal); err != nil {
		return TaskWithRelations{}, err
	}

	row := tx.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if err != nil {
		return TaskWithRelations{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TaskWithRelations{}, fmt.Errorf("commit create task: %w", err)
	}

	slog.Info("task created", "task_id", out.Task.ID, "number", out.Task.Number, "title", out.Task.Title,
		"status", out.Task.Status, "priority", out.Task.Priority, "assignee_id", out.Task.AssigneeID,
		"creator_id", out.Task.CreatorID, "label_count", len(out.Labels))
	return out, nil
}

// GetByNumber returns one task by its human-facing sequential number, or
// domain.ErrNotFound.
func (s *TaskStore) GetByNumber(ctx context.Context, number int64) (TaskWithRelations, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.number = $1`, number)
	out, err := scanTaskWithRelations(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWithRelations{}, err
	}
	return out, nil
}

// Update applies patch to the task named by number, recording one activity
// entry per field that actually changed. The read-modify-write happens
// under a row lock inside one transaction, so the before/after diff the
// activity log records is always the diff that was actually applied — the
// same code path performs the change and writes the record of it, so they
// cannot drift apart.
//
// completed_at is bookkeeping, not caller-settable: it is set the moment
// status becomes "done" and cleared the moment status leaves "done", so it
// always answers "when did this last become done" without the caller having
// to manage it, and without it being a gate on any other field.
func (s *TaskStore) Update(ctx context.Context, number int64, patch domain.TaskPatch, actorID uuid.UUID) (TaskWithRelations, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("begin update task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var (
		id                       uuid.UUID
		oldTitle, oldDescription string
		oldStatus                domain.TaskStatus
		oldPriority              domain.TaskPriority
		oldAssignee              *uuid.UUID
		oldDueDate               *time.Time
		oldProject               *uuid.UUID
		oldMilestone             *uuid.UUID
		oldCompletedAt           *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT id, title, description, status, priority, assignee_id, due_date,
		        project_id, milestone_id, completed_at
		 FROM tasks WHERE number = $1 FOR UPDATE`, number,
	).Scan(&id, &oldTitle, &oldDescription, &oldStatus, &oldPriority, &oldAssignee,
		&oldDueDate, &oldProject, &oldMilestone, &oldCompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("lookup task %d for update: %w", number, err)
	}

	newTitle, newDescription := oldTitle, oldDescription
	newStatus, newPriority := oldStatus, oldPriority
	newAssignee, newDueDate := oldAssignee, oldDueDate
	newProject, newMilestone := oldProject, oldMilestone

	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return TaskWithRelations{}, fmt.Errorf("%w: title is required", domain.ErrInvalidInput)
		}
		newTitle = *patch.Title
	}
	if patch.Description != nil {
		newDescription = *patch.Description
	}
	if patch.Status != nil {
		if !patch.Status.Valid() {
			return TaskWithRelations{}, fmt.Errorf("%w: unknown status %q", domain.ErrInvalidInput, *patch.Status)
		}
		newStatus = *patch.Status
	}
	if patch.Priority != nil {
		if !patch.Priority.Valid() {
			return TaskWithRelations{}, fmt.Errorf("%w: unknown priority %q", domain.ErrInvalidInput, *patch.Priority)
		}
		newPriority = *patch.Priority
	}
	if patch.AssigneeSet {
		newAssignee = patch.AssigneeID
	}
	if patch.DueDateSet {
		newDueDate = patch.DueDate
	}
	if patch.ProjectSet {
		projectChanged := uuidPtrString(oldProject) != uuidPtrString(patch.ProjectID)
		newProject = patch.ProjectID
		if projectChanged && !patch.MilestoneSet {
			newMilestone = nil
		}
	}
	if patch.MilestoneSet {
		newMilestone = patch.MilestoneID
	}
	if newProject == nil && newMilestone != nil {
		return TaskWithRelations{}, fmt.Errorf("%w: a milestone requires a project", domain.ErrInvalidInput)
	}

	newCompletedAt := oldCompletedAt
	if oldStatus != domain.TaskStatusDone && newStatus == domain.TaskStatusDone {
		var readiness domain.TaskCompletionReadiness
		if err := tx.QueryRow(ctx, `
			SELECT count(*),
				count(*) FILTER (
					WHERE latest.outcome IS NULL
						OR latest.outcome NOT IN ('passed', 'waived')
				)
			FROM acceptance_criteria ac
			LEFT JOIN LATERAL (
				SELECT chk.outcome
				FROM acceptance_checks chk
				WHERE chk.criterion_id=ac.id
					AND chk.criterion_revision=ac.revision
				ORDER BY chk.checked_at DESC, chk.id DESC
				LIMIT 1
			) latest ON true
			WHERE ac.task_id=$1 AND ac.archived_at IS NULL`,
			id,
		).Scan(&readiness.ActiveCriteria, &readiness.UnsatisfiedCriteria); err != nil {
			return TaskWithRelations{}, fmt.Errorf("read task acceptance readiness: %w", err)
		}
		if err := (domain.Task{ID: id, Status: oldStatus}).ValidateCompletion(readiness); err != nil {
			return TaskWithRelations{}, err
		}
		now := time.Now().UTC()
		newCompletedAt = &now
	} else if oldStatus == domain.TaskStatusDone && newStatus != domain.TaskStatusDone {
		newCompletedAt = nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tasks SET
			title=$2, description=$3, status=$4, priority=$5,
			assignee_id=$6, due_date=$7, project_id=$8, milestone_id=$9,
			completed_at=$10, updated_at=now()
		WHERE id=$1`,
		id, newTitle, newDescription, string(newStatus), string(newPriority),
		newAssignee, newDueDate, newProject, newMilestone, newCompletedAt,
	); err != nil {
		return TaskWithRelations{}, mapPgError(err)
	}

	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldTitle, oldTitle, newTitle); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldDescription, oldDescription, newDescription); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldStatus, string(oldStatus), string(newStatus)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldPriority, string(oldPriority), string(newPriority)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldAssignee, uuidPtrString(oldAssignee), uuidPtrString(newAssignee)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldDueDate, datePtrString(oldDueDate), datePtrString(newDueDate)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldProject, uuidPtrString(oldProject), uuidPtrString(newProject)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldMilestone, uuidPtrString(oldMilestone), uuidPtrString(newMilestone)); err != nil {
		return TaskWithRelations{}, err
	}

	if patch.LabelsSet {
		oldNames, err := labelNamesForTask(ctx, tx, id)
		if err != nil {
			return TaskWithRelations{}, err
		}
		if err := replaceTaskLabels(ctx, tx, id, patch.LabelIDs); err != nil {
			return TaskWithRelations{}, err
		}
		newNames, err := labelNamesForTask(ctx, tx, id)
		if err != nil {
			return TaskWithRelations{}, err
		}
		if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldLabels, strings.Join(oldNames, ", "), strings.Join(newNames, ", ")); err != nil {
			return TaskWithRelations{}, err
		}
	}

	row := tx.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if err != nil {
		return TaskWithRelations{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TaskWithRelations{}, fmt.Errorf("commit update task: %w", err)
	}

	slog.Info("task updated", "task_id", id, "number", number, "actor_id", actorID,
		"status", out.Task.Status, "priority", out.Task.Priority, "assignee_id", out.Task.AssigneeID,
		"due_date", out.Task.DueDate, "title", out.Task.Title)
	return out, nil
}

// SetArchived sets or clears archived_at for the task named by number.
// Calling it with the value already in effect is a harmless no-op (no new
// activity entry, no error) — archiving is a state, not a one-shot action,
// so re-asserting it is not a client error.
func (s *TaskStore) SetArchived(ctx context.Context, number int64, archived bool, actorID uuid.UUID) (TaskWithRelations, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("begin set archived: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var (
		id          uuid.UUID
		wasArchived bool
	)
	err = tx.QueryRow(ctx, `SELECT id, archived_at IS NOT NULL FROM tasks WHERE number = $1 FOR UPDATE`, number).
		Scan(&id, &wasArchived)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("lookup task %d for archive: %w", number, err)
	}

	if wasArchived != archived {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET archived_at = CASE WHEN $2 THEN now() ELSE NULL END, updated_at = now() WHERE id = $1`,
			id, archived); err != nil {
			return TaskWithRelations{}, fmt.Errorf("set archived on task %d: %w", number, err)
		}
		if err := recordFieldChange(ctx, tx, id, actorID, domain.ActivityFieldArchived, strconv.FormatBool(wasArchived), strconv.FormatBool(archived)); err != nil {
			return TaskWithRelations{}, err
		}
	}

	row := tx.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if err != nil {
		return TaskWithRelations{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TaskWithRelations{}, fmt.Errorf("commit set archived: %w", err)
	}

	slog.Info("task archived flag changed", "task_id", id, "number", number, "actor_id", actorID, "archived", archived)
	return out, nil
}

// --- helpers ---

func replaceTaskLabels(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, labelIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM task_labels WHERE task_id = $1`, taskID); err != nil {
		return fmt.Errorf("clear labels for task %s: %w", taskID, err)
	}
	for _, labelID := range labelIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO task_labels (task_id, label_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, taskID, labelID); err != nil {
			return mapPgError(err)
		}
	}
	return nil
}

func labelNamesForTask(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT l.name FROM task_labels tl JOIN labels l ON l.id = tl.label_id WHERE tl.task_id = $1 ORDER BY l.name`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list label names for task %s: %w", taskID, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan label name: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func insertActivity(ctx context.Context, tx pgx.Tx, taskID, actorID uuid.UUID, field domain.ActivityField, oldValue, newValue *string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO task_activity (id, task_id, actor_id, field, old_value, new_value) VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.New(), taskID, actorID, string(field), oldValue, newValue)
	if err != nil {
		return fmt.Errorf("insert activity %s for task %s: %w", field, taskID, err)
	}
	return nil
}

// recordFieldChange writes an activity entry only when oldVal != newVal,
// keeping the log free of no-op noise from a PATCH that resends a field's
// current value unchanged.
func recordFieldChange(ctx context.Context, tx pgx.Tx, taskID, actorID uuid.UUID, field domain.ActivityField, oldVal, newVal string) error {
	if oldVal == newVal {
		return nil
	}
	return insertActivity(ctx, tx, taskID, actorID, field, strPtrOrNil(oldVal), strPtrOrNil(newVal))
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func datePtrString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// ListActivity returns every activity entry for a task, oldest first, so it
// reads as a chronological timeline of what happened to the task.
func (s *TaskStore) ListActivity(ctx context.Context, taskID uuid.UUID) ([]domain.Activity, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, task_id, actor_id, field, old_value, new_value, created_at
		 FROM task_activity WHERE task_id = $1 ORDER BY created_at ASC, id ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query activity for task %s: %w", taskID, err)
	}
	defer rows.Close()

	out := []domain.Activity{}
	for rows.Next() {
		var a domain.Activity
		if err := rows.Scan(&a.ID, &a.TaskID, &a.ActorID, &a.Field, &a.OldValue, &a.NewValue, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan activity: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- list, filter, sort, keyset pagination ---

// TaskListFilter narrows and orders a List call. The zero value lists every
// non-archived task, newest first, unpaginated (up to the default page
// size).
type TaskListFilter struct {
	Statuses   []domain.TaskStatus
	Priorities []domain.TaskPriority

	// AssigneeID filters to tasks assigned to one user. Unassigned, if true,
	// overrides AssigneeID and filters to tasks with no assignee instead —
	// unassigned must be a first-class, filterable state, not an omission.
	AssigneeID *uuid.UUID
	Unassigned bool

	LabelIDs  []uuid.UUID
	Search    string
	ProjectID *uuid.UUID

	// Archived selects which tasks the archived_at column admits: "" (the
	// zero value) excludes archived tasks, "only" returns exclusively
	// archived tasks, "all" applies no filter on it.
	Archived string

	// Sort is one of created_at, updated_at, due_date, priority, number.
	// Order is asc or desc. Both default when empty.
	Sort  string
	Order string

	Cursor string
	Limit  int
}

// TaskListResult is one page of List, with enough information to fetch the
// next page without an OFFSET (which degrades, and drifts under concurrent
// inserts, as a list grows past a few thousand rows).
type TaskListResult struct {
	Items      []TaskWithRelations
	NextCursor string
	HasMore    bool
}

// taskCursor is the decoded shape of a List page token: the sort field it
// was issued for (so a client can't silently mix cursors across sorts), the
// primary sort value at the boundary row, and that row's Number as the
// tiebreaker for values the primary sort shares with other rows.
type taskCursor struct {
	Sort    string `json:"s"`
	Primary string `json:"p"`
	Number  int64  `json:"n"`
}

func encodeTaskCursor(c taskCursor) string {
	b, _ := json.Marshal(c) // c's fields are all plain strings/ints; Marshal cannot fail here
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeTaskCursor(raw, expectSort string) (taskCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return taskCursor{}, errors.New("malformed cursor")
	}
	var c taskCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return taskCursor{}, errors.New("malformed cursor")
	}
	if c.Sort != expectSort {
		return taskCursor{}, fmt.Errorf("cursor was issued for sort %q, not %q", c.Sort, expectSort)
	}
	return c, nil
}

var validSortFields = map[string]string{
	"created_at": "t.created_at",
	"updated_at": "t.updated_at",
	"number":     "t.number",
	"priority":   "CASE t.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END",
}

// List returns one page of tasks matching f, with every task's creator,
// assignee and labels already embedded — the query a list view needs in a
// single round trip.
//
// Pagination is keyset (cursor), not OFFSET: every sortable expression is
// paired with the task's Number as a tiebreaker, so "WHERE (sort_expr,
// number) < (last_seen, last_number)" identifies the next page directly by
// an indexed comparison instead of by counting and skipping rows. This
// keeps a page fetch's cost independent of how deep into the list it is —
// unlike OFFSET, which must walk and discard every prior row — and immune
// to page drift when rows are inserted between page fetches.
func (s *TaskStore) List(ctx context.Context, f TaskListFilter) (TaskListResult, error) {
	sortField := f.Sort
	if sortField == "" {
		sortField = "created_at"
	}
	order := strings.ToUpper(f.Order)
	if order != "ASC" {
		order = "DESC"
	}

	var primaryExpr, primaryCast string
	switch sortField {
	case "created_at", "updated_at", "number":
		primaryExpr = validSortFields[sortField]
		primaryCast = map[string]string{"created_at": "timestamptz", "updated_at": "timestamptz", "number": "bigint"}[sortField]
	case "priority":
		primaryExpr = validSortFields[sortField]
		primaryCast = "int"
	case "due_date":
		if order == "ASC" {
			primaryExpr = "COALESCE(t.due_date, DATE '9999-12-31')"
		} else {
			primaryExpr = "COALESCE(t.due_date, DATE '0001-01-01')"
		}
		primaryCast = "date"
	default:
		return TaskListResult{}, fmt.Errorf("%w: unknown sort field %q", domain.ErrInvalidInput, f.Sort)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	switch f.Archived {
	case "only":
		where = append(where, "t.archived_at IS NOT NULL")
	case "all":
		// no filter
	default:
		where = append(where, "t.archived_at IS NULL")
	}

	if len(f.Statuses) > 0 {
		where = append(where, "t.status = ANY("+arg(statusStrings(f.Statuses))+"::text[])")
	}
	if len(f.Priorities) > 0 {
		where = append(where, "t.priority = ANY("+arg(priorityStrings(f.Priorities))+"::text[])")
	}
	if f.Unassigned {
		where = append(where, "t.assignee_id IS NULL")
	} else if f.AssigneeID != nil {
		where = append(where, "t.assignee_id = "+arg(*f.AssigneeID))
	}
	if len(f.LabelIDs) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM task_labels tl WHERE tl.task_id = t.id AND tl.label_id = ANY("+arg(f.LabelIDs)+"::uuid[]))")
	}
	if f.ProjectID != nil {
		where = append(where, "t.project_id = "+arg(*f.ProjectID))
	}
	if strings.TrimSpace(f.Search) != "" {
		needle := "%" + escapeLike(f.Search) + "%"
		placeholder := arg(needle)
		where = append(where, fmt.Sprintf("(t.title ILIKE %s ESCAPE '\\' OR t.description ILIKE %s ESCAPE '\\')", placeholder, placeholder))
	}
	if f.Cursor != "" {
		cur, err := decodeTaskCursor(f.Cursor, sortField)
		if err != nil {
			return TaskListResult{}, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err)
		}
		cmp := "<"
		if order == "ASC" {
			cmp = ">"
		}
		primaryPlaceholder := arg(cur.Primary) + "::" + primaryCast
		numberPlaceholder := arg(cur.Number)
		where = append(where, fmt.Sprintf("ROW(%s, t.number) %s ROW(%s, %s)", primaryExpr, cmp, primaryPlaceholder, numberPlaceholder))
	}

	query := `SELECT ` + taskSelectColumns + ` ` + taskFromJoins
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY %s %s, t.number %s LIMIT %s", primaryExpr, order, order, arg(limit+1))

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return TaskListResult{}, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	items := []TaskWithRelations{}
	for rows.Next() {
		twr, err := scanTaskWithRelations(rows)
		if err != nil {
			return TaskListResult{}, err
		}
		items = append(items, twr)
	}
	if err := rows.Err(); err != nil {
		return TaskListResult{}, fmt.Errorf("list tasks: %w", err)
	}

	result := TaskListResult{Items: items}
	if len(items) > limit {
		result.HasMore = true
		result.Items = items[:limit]
	}
	if result.HasMore {
		last := result.Items[len(result.Items)-1].Task
		result.NextCursor = encodeTaskCursor(taskCursor{
			Sort:    sortField,
			Primary: taskPrimaryValueString(last, sortField, order),
			Number:  last.Number,
		})
	}
	return result, nil
}

// taskPrimaryValueString computes, in Go, the same value the SQL primaryExpr
// for sortField would have computed for this row — so the cursor issued for
// the last row of a page compares correctly against the next page's WHERE
// clause.
func taskPrimaryValueString(t domain.Task, sortField, order string) string {
	switch sortField {
	case "created_at":
		return t.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updated_at":
		return t.UpdatedAt.UTC().Format(time.RFC3339Nano)
	case "number":
		return strconv.FormatInt(t.Number, 10)
	case "priority":
		return strconv.Itoa(priorityRank(t.Priority))
	case "due_date":
		if t.DueDate != nil {
			return t.DueDate.Format("2006-01-02")
		}
		if order == "ASC" {
			return "9999-12-31"
		}
		return "0001-01-01"
	}
	return ""
}

func priorityRank(p domain.TaskPriority) int {
	switch p {
	case domain.TaskPriorityUrgent:
		return 4
	case domain.TaskPriorityHigh:
		return 3
	case domain.TaskPriorityMedium:
		return 2
	case domain.TaskPriorityLow:
		return 1
	default:
		return 0
	}
}

func statusStrings(ss []domain.TaskStatus) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

func priorityStrings(ps []domain.TaskPriority) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

// escapeLike escapes the ILIKE metacharacters %, _ and \ in user-supplied
// search text so a query for e.g. "50%" or "a_b" matches literally instead
// of being interpreted as a wildcard.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
