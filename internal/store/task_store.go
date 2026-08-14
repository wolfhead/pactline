package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TaskStore reads and writes tasks, their labels and their activity log.
type TaskStore struct{ db *DB }

// NewTaskStore wires a TaskStore to the pool.
func NewTaskStore(db *DB) *TaskStore { return &TaskStore{db: db} }

const taskColumns = `t.id, t.number, t.version, t.title, t.context, t.expected_result,
	t.description, t.phase, t.activity_state, t.review_cycle, t.active_issue_thread_id,
	main_thread.id,
	t.status, t.priority, t.execution_mode,
	t.assignee_id, t.creator_id, t.start_date, t.due_date, t.project_id, t.milestone_id,
	t.parent_task_id,
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
	COALESCE(lbl.labels, '[]'::json),
	COALESCE(parent_rel.task, 'null'::json),
	COALESCE(child_rel.tasks, '[]'::json),
	COALESCE(dependency_rel.tasks, '[]'::json),
	COALESCE(dependent_rel.tasks, '[]'::json),
	COALESCE(agent_work.claim, 'null'::json)`

const taskFromJoins = `FROM tasks t
	JOIN users cu ON cu.id = t.creator_id
	LEFT JOIN users au ON au.id = t.assignee_id
	LEFT JOIN task_threads main_thread
		ON main_thread.task_id=t.id AND main_thread.role='main'
	JOIN projects p ON p.id = t.project_id
	LEFT JOIN milestones m ON m.id = t.milestone_id
	LEFT JOIN LATERAL (
		SELECT json_agg(json_build_object(
			'id', l.id, 'name', l.name, 'version', l.version, 'created_at', l.created_at
		) ORDER BY l.name) AS labels
		FROM task_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.task_id = t.id
	) lbl ON true
	LEFT JOIN LATERAL (
		SELECT json_build_object(
			'id', related.id,
			'number', related.number,
			'title', related.title,
			'phase', related.phase,
			'archived', related.archived_at IS NOT NULL,
			'milestone', CASE
				WHEN related_milestone.id IS NULL THEN NULL
				ELSE json_build_object(
					'id', related_milestone.id,
					'name', related_milestone.name
				)
			END
		) AS task
		FROM tasks related
		LEFT JOIN milestones related_milestone ON related_milestone.id = related.milestone_id
		WHERE related.id = t.parent_task_id
	) parent_rel ON true
	LEFT JOIN LATERAL (
		SELECT json_agg(json_build_object(
			'id', related.id,
			'number', related.number,
			'title', related.title,
			'phase', related.phase,
			'archived', related.archived_at IS NOT NULL,
			'milestone', CASE
				WHEN related_milestone.id IS NULL THEN NULL
				ELSE json_build_object(
					'id', related_milestone.id,
					'name', related_milestone.name
				)
			END
		) ORDER BY related.number) AS tasks
		FROM tasks related
		LEFT JOIN milestones related_milestone ON related_milestone.id = related.milestone_id
		WHERE related.parent_task_id = t.id
	) child_rel ON true
	LEFT JOIN LATERAL (
		SELECT json_agg(json_build_object(
			'id', related.id,
			'number', related.number,
			'title', related.title,
			'phase', related.phase,
			'archived', related.archived_at IS NOT NULL,
			'milestone', CASE
				WHEN related_milestone.id IS NULL THEN NULL
				ELSE json_build_object(
					'id', related_milestone.id,
					'name', related_milestone.name
				)
			END
		) ORDER BY related.number) AS tasks
		FROM task_dependencies dependency
		JOIN tasks related ON related.id = dependency.depends_on_task_id
		LEFT JOIN milestones related_milestone ON related_milestone.id = related.milestone_id
		WHERE dependency.task_id = t.id
	) dependency_rel ON true
	LEFT JOIN LATERAL (
		SELECT json_agg(json_build_object(
			'id', related.id,
			'number', related.number,
			'title', related.title,
			'phase', related.phase,
			'archived', related.archived_at IS NOT NULL,
			'milestone', CASE
				WHEN related_milestone.id IS NULL THEN NULL
				ELSE json_build_object(
					'id', related_milestone.id,
					'name', related_milestone.name
				)
			END
		) ORDER BY related.number) AS tasks
		FROM task_dependencies dependency
		JOIN tasks related ON related.id = dependency.task_id
		LEFT JOIN milestones related_milestone ON related_milestone.id = related.milestone_id
		WHERE dependency.depends_on_task_id = t.id
	) dependent_rel ON true
	LEFT JOIN LATERAL (
		SELECT json_build_object(
			'claim_id', claim.id,
			'status', claim.status,
			'token_name', claim.token_name_snapshot,
			'client_kind', claim.client_kind,
			'updated_at', claim.updated_at,
			'completed_at', claim.completed_at
		) AS claim
		FROM task_claims claim
		WHERE claim.task_id = t.id
		  AND (
			claim.status IN ('active', 'waiting_human')
			OR (claim.status = 'submitted' AND t.status = 'in_review')
		  )
		ORDER BY
			CASE WHEN claim.status IN ('active', 'waiting_human') THEN 0 ELSE 1 END,
			claim.updated_at DESC,
			claim.id DESC
		LIMIT 1
	) agent_work ON true`

// TaskWithRelations is a task together with the entities a frontend needs
// alongside it in one response: the creator, required Project, the assignee
// (nil when unassigned — a normal, first-class state), and every label it
// wears.
type TaskWithRelations struct {
	Task         domain.Task
	Creator      domain.UserRef
	Assignee     *domain.UserRef
	Project      ProjectRef
	Milestone    *MilestoneRef
	Labels       []domain.Label
	Parent       *TaskRelationRef
	Children     []TaskRelationRef
	Dependencies []TaskRelationRef
	Dependents   []TaskRelationRef
	AgentWork    *TaskAgentWorkSummary
	Blocked      bool
}

// TaskAgentWorkSummary is the read-model projection needed to show current
// external Agent activity beside a Task. It deliberately excludes session
// identity and Claim mutation fields: the list needs state, not ownership
// credentials or a second Claim API.
type TaskAgentWorkSummary struct {
	ClaimID     uuid.UUID              `json:"claim_id"`
	Status      domain.TaskClaimStatus `json:"status"`
	TokenName   string                 `json:"token_name"`
	ClientKind  string                 `json:"client_kind"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at"`
}

type ProjectRef struct {
	ID     uuid.UUID
	Number int64
	Name   string
}

type MilestoneRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type TaskRelationRef struct {
	ID        uuid.UUID        `json:"id"`
	Number    int64            `json:"number"`
	Title     string           `json:"title"`
	Phase     domain.TaskPhase `json:"phase"`
	Archived  bool             `json:"archived"`
	Milestone *MilestoneRef    `json:"milestone"`
}

func scanTaskWithRelations(s scanner) (TaskWithRelations, error) {
	var (
		twr              TaskWithRelations
		assigneeID       *uuid.UUID
		assigneeName     *string
		assigneeEmail    *string
		milestoneID      *uuid.UUID
		milestoneName    *string
		labelsJSON       []byte
		parentJSON       []byte
		childrenJSON     []byte
		dependenciesJSON []byte
		dependentsJSON   []byte
		agentWorkJSON    []byte
		phase            *string
		activity         *string
		reviewCycle      *int64
		mainThreadID     *uuid.UUID
	)
	err := s.Scan(
		&twr.Task.ID, &twr.Task.Number, &twr.Task.Version,
		&twr.Task.Title, &twr.Task.Context, &twr.Task.ExpectedResult,
		&twr.Task.Description, &phase, &activity, &reviewCycle,
		&twr.Task.ActiveIssueThreadID,
		&mainThreadID,
		&twr.Task.Status, &twr.Task.Priority, &twr.Task.ExecutionMode,
		&twr.Task.AssigneeID, &twr.Task.CreatorID, &twr.Task.StartDate, &twr.Task.DueDate,
		&twr.Task.ProjectID, &twr.Task.MilestoneID, &twr.Task.ParentTaskID,
		&twr.Task.CreatedAt, &twr.Task.UpdatedAt,
		&twr.Task.CompletedAt, &twr.Task.ArchivedAt,
		&twr.Creator.ID, &twr.Creator.Name, &twr.Creator.Email,
		&assigneeID, &assigneeName, &assigneeEmail,
		&twr.Project.ID, &twr.Project.Number, &twr.Project.Name,
		&milestoneID, &milestoneName,
		&labelsJSON,
		&parentJSON, &childrenJSON, &dependenciesJSON, &dependentsJSON,
		&agentWorkJSON,
	)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("scan task: %w", err)
	}
	if phase == nil || reviewCycle == nil || mainThreadID == nil {
		return TaskWithRelations{}, fmt.Errorf(
			"%w: Task %d has not been classified into the target workflow",
			domain.ErrMigrationRequired,
			twr.Task.Number,
		)
	}
	if assigneeID != nil {
		twr.Assignee = &domain.UserRef{ID: *assigneeID, Name: derefStr(assigneeName), Email: assigneeEmail}
	}
	twr.Task.Phase = domain.TaskPhase(*phase)
	if activity != nil {
		twr.Task.Activity = domain.TaskActivityState(*activity)
	}
	twr.Task.ReviewCycle = *reviewCycle
	twr.Task.MainThreadID = *mainThreadID
	if milestoneID != nil {
		twr.Milestone = &MilestoneRef{ID: *milestoneID, Name: derefStr(milestoneName)}
	}
	labels := []domain.Label{}
	if err := json.Unmarshal(labelsJSON, &labels); err != nil {
		return TaskWithRelations{}, fmt.Errorf("unmarshal labels for task %s: %w", twr.Task.ID, err)
	}
	twr.Labels = labels
	if len(parentJSON) > 0 && string(parentJSON) != "null" {
		var parent TaskRelationRef
		if err := json.Unmarshal(parentJSON, &parent); err != nil {
			return TaskWithRelations{}, fmt.Errorf("unmarshal parent for task %s: %w", twr.Task.ID, err)
		}
		twr.Parent = &parent
	}
	if err := unmarshalTaskRelationRefs(childrenJSON, &twr.Children); err != nil {
		return TaskWithRelations{}, fmt.Errorf("unmarshal children for task %s: %w", twr.Task.ID, err)
	}
	if err := unmarshalTaskRelationRefs(dependenciesJSON, &twr.Dependencies); err != nil {
		return TaskWithRelations{}, fmt.Errorf("unmarshal dependencies for task %s: %w", twr.Task.ID, err)
	}
	if err := unmarshalTaskRelationRefs(dependentsJSON, &twr.Dependents); err != nil {
		return TaskWithRelations{}, fmt.Errorf("unmarshal dependents for task %s: %w", twr.Task.ID, err)
	}
	if len(agentWorkJSON) > 0 && string(agentWorkJSON) != "null" {
		var agentWork TaskAgentWorkSummary
		if err := json.Unmarshal(agentWorkJSON, &agentWork); err != nil {
			return TaskWithRelations{}, fmt.Errorf(
				"unmarshal Agent work summary for task %s: %w", twr.Task.ID, err,
			)
		}
		twr.AgentWork = &agentWork
	}
	for _, dependency := range twr.Dependencies {
		if dependency.Phase != domain.TaskPhaseDone && dependency.Phase != domain.TaskPhaseCancelled {
			twr.Blocked = true
			break
		}
	}
	return twr, nil
}

func unmarshalTaskRelationRefs(raw []byte, target *[]TaskRelationRef) error {
	*target = []TaskRelationRef{}
	return json.Unmarshal(raw, target)
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
	return s.CreateWithOperation(ctx, t, labelIDs, nil, domain.SessionOperation(t.CreatorID, "internal"))
}

func (s *TaskStore) CreateWithOperation(
	ctx context.Context,
	t domain.Task,
	labelIDs []uuid.UUID,
	dependencyIDs []uuid.UUID,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	if err := actor.Validate(); err != nil {
		return TaskWithRelations{}, err
	}
	if t.CreatorID != actor.UserID {
		return TaskWithRelations{}, fmt.Errorf("%w: task creator must match operation actor", domain.ErrForbidden)
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if strings.TrimSpace(t.Title) == "" {
		return TaskWithRelations{}, fmt.Errorf("%w: title is required", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(t.Context) == "" {
		return TaskWithRelations{}, fmt.Errorf("%w: task context is required", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(t.ExpectedResult) == "" {
		return TaskWithRelations{}, fmt.Errorf("%w: task expected result is required", domain.ErrInvalidInput)
	}
	if t.Status == "" {
		t.Status = domain.TaskStatusTodo
	}
	if t.Priority == "" {
		t.Priority = domain.TaskPriorityNone
	}
	if t.ExecutionMode == "" {
		t.ExecutionMode = domain.TaskExecutionModeHumanOnly
	}
	if !t.Status.Valid() {
		return TaskWithRelations{}, fmt.Errorf("%w: unknown status %q", domain.ErrInvalidInput, t.Status)
	}
	if !t.Priority.Valid() {
		return TaskWithRelations{}, fmt.Errorf("%w: unknown priority %q", domain.ErrInvalidInput, t.Priority)
	}
	if !t.ExecutionMode.Valid() {
		return TaskWithRelations{}, fmt.Errorf(
			"%w: unknown execution mode %q", domain.ErrInvalidInput, t.ExecutionMode,
		)
	}
	if t.ProjectID == uuid.Nil {
		return TaskWithRelations{}, fmt.Errorf("%w: task project is required", domain.ErrInvalidInput)
	}
	if err := domain.ValidateSchedule(t.StartDate, t.DueDate); err != nil {
		return TaskWithRelations{}, err
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

	if err := validateParentAssignment(
		ctx, tx, t.ID, t.ParentTaskID, t.ProjectID, t.MilestoneID, t.Status,
	); err != nil {
		return TaskWithRelations{}, err
	}
	if err := validateDependencyAssignments(
		ctx, tx, t.ID, t.ProjectID, t.ParentTaskID, dependencyIDs,
	); err != nil {
		return TaskWithRelations{}, err
	}
	if t.Status == domain.TaskStatusDone {
		unfinished, err := countUnfinishedTasks(ctx, tx, dependencyIDs)
		if err != nil {
			return TaskWithRelations{}, err
		}
		if err := t.ValidateCompletion(domain.TaskCompletionReadiness{
			UnfinishedDependencies: unfinished,
		}); err != nil {
			return TaskWithRelations{}, err
		}
	}

	var (
		id     uuid.UUID
		number int64
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks
			(id, title, context, expected_result, description, status, priority,
			 execution_mode, assignee_id, creator_id, start_date, due_date,
			 project_id, milestone_id, parent_task_id, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, number`,
		t.ID, t.Title, t.Context, t.ExpectedResult, t.Description,
		string(t.Status), string(t.Priority), string(t.ExecutionMode), t.AssigneeID, t.CreatorID,
		t.StartDate, t.DueDate, t.ProjectID, t.MilestoneID, t.ParentTaskID, completedAt,
	).Scan(&id, &number)
	if err != nil {
		return TaskWithRelations{}, mapPgError(err)
	}

	if len(labelIDs) > 0 {
		if err := replaceTaskLabels(ctx, tx, id, labelIDs); err != nil {
			return TaskWithRelations{}, err
		}
	}
	if err := replaceTaskDependencies(ctx, tx, id, dependencyIDs); err != nil {
		return TaskWithRelations{}, err
	}

	statusVal := string(t.Status)
	if err := insertActivity(ctx, tx, id, actor, domain.ActivityFieldCreated, nil, &statusVal); err != nil {
		return TaskWithRelations{}, err
	}
	newValue, _ := json.Marshal(map[string]any{
		"title": t.Title, "context": t.Context, "expected_result": t.ExpectedResult,
		"description": t.Description, "status": t.Status,
		"priority": t.Priority, "execution_mode": t.ExecutionMode,
		"assignee_id": t.AssigneeID,
		"start_date":  t.StartDate, "due_date": t.DueDate,
		"project_id": t.ProjectID, "milestone_id": t.MilestoneID,
		"parent_task_id": t.ParentTaskID, "label_ids": labelIDs,
		"dependency_ids": dependencyIDs,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "task",
		EntityID: id, EntityNumber: &number, Action: "created", NewValue: newValue,
	}); err != nil {
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

func (s *TaskStore) GetByID(ctx context.Context, id uuid.UUID) (TaskWithRelations, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	return out, err
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
func (s *TaskStore) Update(
	ctx context.Context,
	number int64,
	patch domain.TaskPatch,
	actorID uuid.UUID,
) (TaskWithRelations, error) {
	return s.UpdateWithOperation(ctx, number, patch, domain.SessionOperation(actorID, "internal"))
}

func (s *TaskStore) UpdateWithOperation(
	ctx context.Context,
	number int64,
	patch domain.TaskPatch,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	return s.updateWithExpectedVersion(ctx, number, nil, patch, actor)
}

func (s *TaskStore) UpdateVersionedWithOperation(
	ctx context.Context,
	number, expectedVersion int64,
	patch domain.TaskPatch,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	return s.updateWithExpectedVersion(ctx, number, &expectedVersion, patch, actor)
}

func (s *TaskStore) updateWithExpectedVersion(
	ctx context.Context,
	number int64,
	expectedVersion *int64,
	patch domain.TaskPatch,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	if err := actor.Validate(); err != nil {
		return TaskWithRelations{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("begin update task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var (
		id                                                      uuid.UUID
		oldTitle, oldContext, oldExpectedResult, oldDescription string
		oldStatus                                               domain.TaskStatus
		oldPriority                                             domain.TaskPriority
		oldExecutionMode                                        domain.TaskExecutionMode
		oldAssignee                                             *uuid.UUID
		oldStartDate                                            *time.Time
		oldDueDate                                              *time.Time
		oldProject                                              uuid.UUID
		oldMilestone                                            *uuid.UUID
		oldParent                                               *uuid.UUID
		oldCompletedAt                                          *time.Time
		oldVersion                                              int64
		oldPhase                                                *string
	)
	err = tx.QueryRow(ctx,
		`SELECT id, version, title, context, expected_result, description,
		        status, priority, execution_mode, assignee_id, start_date, due_date,
		        project_id, milestone_id, parent_task_id, completed_at, phase
		 FROM tasks WHERE number = $1 FOR UPDATE`, number,
	).Scan(&id, &oldVersion, &oldTitle, &oldContext, &oldExpectedResult,
		&oldDescription, &oldStatus, &oldPriority, &oldExecutionMode, &oldAssignee,
		&oldStartDate, &oldDueDate, &oldProject, &oldMilestone,
		&oldParent, &oldCompletedAt, &oldPhase)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("lookup task %d for update: %w", number, err)
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return TaskWithRelations{}, domain.VersionConflictError{CurrentVersion: oldVersion}
	}

	newTitle, newContext := oldTitle, oldContext
	newExpectedResult, newDescription := oldExpectedResult, oldDescription
	newStatus, newPriority := oldStatus, oldPriority
	newExecutionMode := oldExecutionMode
	newAssignee, newStartDate, newDueDate := oldAssignee, oldStartDate, oldDueDate
	newProject, newMilestone := oldProject, oldMilestone
	newParent := oldParent

	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return TaskWithRelations{}, fmt.Errorf("%w: title is required", domain.ErrInvalidInput)
		}
		newTitle = *patch.Title
	}
	if patch.Context != nil {
		if strings.TrimSpace(*patch.Context) == "" {
			return TaskWithRelations{}, fmt.Errorf("%w: task context is required", domain.ErrInvalidInput)
		}
		newContext = *patch.Context
	}
	if patch.ExpectedResult != nil {
		if strings.TrimSpace(*patch.ExpectedResult) == "" {
			return TaskWithRelations{}, fmt.Errorf("%w: task expected result is required", domain.ErrInvalidInput)
		}
		newExpectedResult = *patch.ExpectedResult
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
	if patch.ExecutionMode != nil {
		if !patch.ExecutionMode.Valid() {
			return TaskWithRelations{}, fmt.Errorf(
				"%w: unknown execution mode %q",
				domain.ErrInvalidInput,
				*patch.ExecutionMode,
			)
		}
		newExecutionMode = *patch.ExecutionMode
	}
	if patch.AssigneeSet {
		newAssignee = patch.AssigneeID
	}
	if patch.StartDateSet {
		newStartDate = patch.StartDate
	}
	if patch.DueDateSet {
		newDueDate = patch.DueDate
	}
	if patch.ScheduleShiftDays != nil {
		if *patch.ScheduleShiftDays == 0 {
			return TaskWithRelations{}, fmt.Errorf(
				"%w: schedule_shift_days must not be zero",
				domain.ErrInvalidInput,
			)
		}
		if patch.StartDateSet || patch.DueDateSet {
			return TaskWithRelations{}, fmt.Errorf(
				"%w: schedule_shift_days cannot be combined with explicit schedule dates",
				domain.ErrInvalidInput,
			)
		}
		if newStartDate != nil {
			shifted := newStartDate.AddDate(0, 0, *patch.ScheduleShiftDays)
			newStartDate = &shifted
		}
		if newDueDate != nil {
			shifted := newDueDate.AddDate(0, 0, *patch.ScheduleShiftDays)
			newDueDate = &shifted
		}
		if newStartDate == nil && newDueDate == nil {
			var scheduledChildren bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM tasks
					WHERE parent_task_id=$1
					  AND (start_date IS NOT NULL OR due_date IS NOT NULL)
				)`,
				id,
			).Scan(&scheduledChildren); err != nil {
				return TaskWithRelations{}, fmt.Errorf("check child schedules: %w", err)
			}
			if !scheduledChildren {
				return TaskWithRelations{}, fmt.Errorf(
					"%w: an unscheduled task cannot be shifted",
					domain.ErrConflict,
				)
			}
		}
	}
	if patch.ProjectSet {
		return TaskWithRelations{}, fmt.Errorf("%w: Task Project is immutable", domain.ErrConflict)
	}
	if patch.MilestoneSet {
		newMilestone = patch.MilestoneID
	}
	if patch.ParentSet {
		newParent = patch.ParentTaskID
	}
	if newProject == uuid.Nil {
		return TaskWithRelations{}, fmt.Errorf("%w: task project is required", domain.ErrInvalidInput)
	}
	if err := domain.ValidateSchedule(newStartDate, newDueDate); err != nil {
		return TaskWithRelations{}, err
	}
	if oldParent != nil &&
		(oldProject != newProject || !uuidPtrsEqual(oldMilestone, newMilestone)) &&
		(!patch.ParentSet || patch.ParentTaskID != nil) {
		return TaskWithRelations{}, fmt.Errorf(
			"%w: detach a child before moving it to another project or milestone",
			domain.ErrConflict,
		)
	}
	if err := validateParentAssignment(
		ctx, tx, id, newParent, newProject, newMilestone, newStatus,
	); err != nil {
		return TaskWithRelations{}, err
	}
	oldDependencyIDs, err := taskDependencyIDs(ctx, tx, id, true)
	if err != nil {
		return TaskWithRelations{}, err
	}
	newDependencyIDs := oldDependencyIDs
	if patch.DependenciesSet {
		newDependencyIDs = patch.DependencyIDs
		if err := validateDependencyAssignments(
			ctx, tx, id, newProject, newParent, patch.DependencyIDs,
		); err != nil {
			return TaskWithRelations{}, err
		}
		if oldPhase != nil && *oldPhase == string(domain.TaskPhaseReady) {
			unfinished, err := countUnfinishedTasks(ctx, tx, patch.DependencyIDs)
			if err != nil {
				return TaskWithRelations{}, err
			}
			if unfinished > 0 {
				return TaskWithRelations{}, fmt.Errorf(
					"%w: withdraw readiness before adding an unfinished dependency",
					domain.ErrInvalidTransition,
				)
			}
		}
		if err := replaceTaskDependencies(ctx, tx, id, patch.DependencyIDs); err != nil {
			return TaskWithRelations{}, err
		}
	} else if oldProject != newProject {
		if err := validateExistingDependenciesForProject(ctx, tx, id, newProject); err != nil {
			return TaskWithRelations{}, err
		}
	}

	newCompletedAt := oldCompletedAt
	if newStatus == domain.TaskStatusDone &&
		(oldStatus != domain.TaskStatusDone || patch.DependenciesSet) {
		var readiness domain.TaskCompletionReadiness
		if err := tx.QueryRow(ctx, `
			SELECT count(*),
				count(*) FILTER (
					WHERE latest.outcome IS NULL
						OR latest.outcome NOT IN ('passed', 'waived')
				),
				(SELECT count(*)
				 FROM tasks child
				 WHERE child.parent_task_id=$1
				   AND (child.phase IS NULL OR child.phase NOT IN ('done', 'cancelled'))),
				(SELECT count(*)
				 FROM task_dependencies dependency
				 JOIN tasks predecessor ON predecessor.id=dependency.depends_on_task_id
				 WHERE dependency.task_id=$1
				   AND (predecessor.phase IS NULL OR predecessor.phase NOT IN ('done', 'cancelled')))
			FROM acceptance_criteria ac
			LEFT JOIN LATERAL (
				SELECT claim.completed_at
				FROM task_claims claim
				WHERE claim.task_id=$1
					AND claim.status='submitted'
				ORDER BY claim.completed_at DESC, claim.id DESC
				LIMIT 1
			) submission ON true
			LEFT JOIN LATERAL (
				SELECT chk.outcome
				FROM acceptance_checks chk
				WHERE chk.criterion_id=ac.id
					AND chk.criterion_revision=ac.revision
					AND (
						submission.completed_at IS NULL
						OR (
							chk.checker_type='user'
							AND chk.checked_at >= submission.completed_at
						)
					)
				ORDER BY chk.checked_at DESC, chk.id DESC
				LIMIT 1
			) latest ON true
			WHERE ac.task_id=$1 AND ac.archived_at IS NULL`,
			id,
		).Scan(
			&readiness.ActiveCriteria,
			&readiness.UnsatisfiedCriteria,
			&readiness.UnfinishedChildren,
			&readiness.UnfinishedDependencies,
		); err != nil {
			return TaskWithRelations{}, fmt.Errorf("read task acceptance readiness: %w", err)
		}
		if err := (domain.Task{ID: id, Status: oldStatus}).ValidateCompletion(readiness); err != nil {
			return TaskWithRelations{}, err
		}
		if oldStatus != domain.TaskStatusDone {
			now := time.Now().UTC()
			newCompletedAt = &now
		}
	} else if oldStatus == domain.TaskStatusDone && newStatus != domain.TaskStatusDone {
		newCompletedAt = nil
	}

	var nextVersion int64
	if err := tx.QueryRow(ctx, `
		UPDATE tasks SET
			title=$2, context=$3, expected_result=$4, description=$5,
			status=$6, priority=$7, execution_mode=$8, assignee_id=$9,
			start_date=$10, due_date=$11, project_id=$12, milestone_id=$13,
			parent_task_id=$14, completed_at=$15,
			version=version+1, updated_at=now()
		WHERE id=$1 AND version=$16
		RETURNING version`,
		id, newTitle, newContext, newExpectedResult, newDescription,
		string(newStatus), string(newPriority), string(newExecutionMode),
		newAssignee, newStartDate, newDueDate,
		newProject, newMilestone, newParent, newCompletedAt, oldVersion,
	).Scan(&nextVersion); err != nil {
		return TaskWithRelations{}, mapPgError(err)
	}
	if oldParent == nil &&
		(oldProject != newProject || !uuidPtrsEqual(oldMilestone, newMilestone)) {
		if err := moveTaskChildren(
			ctx, tx, id, newProject, newMilestone, actor,
		); err != nil {
			return TaskWithRelations{}, err
		}
	}
	if patch.ScheduleShiftDays != nil && oldParent == nil {
		if err := shiftTaskChildrenSchedules(
			ctx, tx, id, *patch.ScheduleShiftDays, actor,
		); err != nil {
			return TaskWithRelations{}, err
		}
	}

	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldTitle, oldTitle, newTitle); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldContext, oldContext, newContext); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldExpectedResult, oldExpectedResult, newExpectedResult); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldDescription, oldDescription, newDescription); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldStatus, string(oldStatus), string(newStatus)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldPriority, string(oldPriority), string(newPriority)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(
		ctx, tx, id, actor, domain.ActivityFieldExecutionMode,
		string(oldExecutionMode), string(newExecutionMode),
	); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldAssignee, uuidPtrString(oldAssignee), uuidPtrString(newAssignee)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldStartDate, datePtrString(oldStartDate), datePtrString(newStartDate)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldDueDate, datePtrString(oldDueDate), datePtrString(newDueDate)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldProject, oldProject.String(), newProject.String()); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldMilestone, uuidPtrString(oldMilestone), uuidPtrString(newMilestone)); err != nil {
		return TaskWithRelations{}, err
	}
	if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldParent, uuidPtrString(oldParent), uuidPtrString(newParent)); err != nil {
		return TaskWithRelations{}, err
	}
	if patch.DependenciesSet {
		if err := recordFieldChange(
			ctx, tx, id, actor, domain.ActivityFieldDependencies,
			uuidListString(oldDependencyIDs), uuidListString(newDependencyIDs),
		); err != nil {
			return TaskWithRelations{}, err
		}
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
		if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldLabels, strings.Join(oldNames, ", "), strings.Join(newNames, ", ")); err != nil {
			return TaskWithRelations{}, err
		}
	}
	oldValue, _ := json.Marshal(map[string]any{
		"title": oldTitle, "context": oldContext, "expected_result": oldExpectedResult,
		"description": oldDescription, "status": oldStatus,
		"priority": oldPriority, "execution_mode": oldExecutionMode,
		"assignee_id": oldAssignee,
		"start_date":  oldStartDate, "due_date": oldDueDate,
		"project_id": oldProject, "milestone_id": oldMilestone,
		"parent_task_id": oldParent, "dependency_ids": oldDependencyIDs,
	})
	newValue, _ := json.Marshal(map[string]any{
		"title": newTitle, "context": newContext, "expected_result": newExpectedResult,
		"description": newDescription, "status": newStatus,
		"priority": newPriority, "execution_mode": newExecutionMode,
		"assignee_id": newAssignee,
		"start_date":  newStartDate, "due_date": newDueDate,
		"project_id": newProject, "milestone_id": newMilestone,
		"parent_task_id": newParent, "dependency_ids": newDependencyIDs,
		"schedule_shift_days": patch.ScheduleShiftDays,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "task",
		EntityID: id, EntityNumber: &number, Action: "updated",
		OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return TaskWithRelations{}, err
	}

	row := tx.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if err != nil {
		return TaskWithRelations{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TaskWithRelations{}, fmt.Errorf("commit update task: %w", err)
	}

	slog.Info("task updated", "task_id", id, "number", number, "actor_id", actor.UserID,
		"status", out.Task.Status, "priority", out.Task.Priority, "assignee_id", out.Task.AssigneeID,
		"due_date", out.Task.DueDate, "title", out.Task.Title)
	return out, nil
}

// SetArchived sets or clears archived_at for the task named by number.
// Calling it with the value already in effect is a harmless no-op (no new
// activity entry, no error) — archiving is a state, not a one-shot action,
// so re-asserting it is not a client error.
func (s *TaskStore) SetArchived(
	ctx context.Context,
	number int64,
	archived bool,
	actorID uuid.UUID,
) (TaskWithRelations, error) {
	return s.SetArchivedWithOperation(
		ctx, number, archived, domain.SessionOperation(actorID, "internal"),
	)
}

func (s *TaskStore) SetArchivedWithOperation(
	ctx context.Context,
	number int64,
	archived bool,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	return s.setArchivedWithExpectedVersion(ctx, number, nil, archived, actor)
}

func (s *TaskStore) SetArchivedVersionedWithOperation(
	ctx context.Context,
	number, expectedVersion int64,
	archived bool,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	return s.setArchivedWithExpectedVersion(ctx, number, &expectedVersion, archived, actor)
}

func (s *TaskStore) setArchivedWithExpectedVersion(
	ctx context.Context,
	number int64,
	expectedVersion *int64,
	archived bool,
	actor domain.OperationActor,
) (TaskWithRelations, error) {
	if err := actor.Validate(); err != nil {
		return TaskWithRelations{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("begin set archived: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var (
		id          uuid.UUID
		wasArchived bool
		oldVersion  int64
		activity    *string
	)
	err = tx.QueryRow(ctx, `
		SELECT id,version,archived_at IS NOT NULL,activity_state
		FROM tasks WHERE number=$1 FOR UPDATE`, number).
		Scan(&id, &oldVersion, &wasArchived, &activity)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWithRelations{}, fmt.Errorf("lookup task %d for archive: %w", number, err)
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return TaskWithRelations{}, domain.VersionConflictError{CurrentVersion: oldVersion}
	}
	if archived && !wasArchived && activity != nil &&
		(*activity == string(domain.TaskActivityWorking) ||
			*activity == string(domain.TaskActivityNeedsResolution)) {
		return TaskWithRelations{}, fmt.Errorf(
			"%w: release, resolve, or cancel the active Task workflow before archiving",
			domain.ErrConflict,
		)
	}

	if wasArchived != archived {
		if err := recordFieldChange(ctx, tx, id, actor, domain.ActivityFieldArchived, strconv.FormatBool(wasArchived), strconv.FormatBool(archived)); err != nil {
			return TaskWithRelations{}, err
		}
		oldValue, _ := json.Marshal(map[string]bool{"archived": wasArchived})
		newValue, _ := json.Marshal(map[string]bool{"archived": archived})
		action := "archived"
		if !archived {
			action = "restored"
		}
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "task",
			EntityID: id, EntityNumber: &number, Action: action,
			OldValue: oldValue, NewValue: newValue,
		}); err != nil {
			return TaskWithRelations{}, err
		}
	}
	var nextVersion int64
	if err := tx.QueryRow(ctx, `
		UPDATE tasks
		SET archived_at = CASE WHEN $2 THEN COALESCE(archived_at, now()) ELSE NULL END,
		    version=version+1, updated_at=now()
		WHERE id=$1 AND version=$3
		RETURNING version`,
		id, archived, oldVersion,
	).Scan(&nextVersion); err != nil {
		return TaskWithRelations{}, fmt.Errorf("set archived on task %d: %w", number, err)
	}

	row := tx.QueryRow(ctx, `SELECT `+taskSelectColumns+` `+taskFromJoins+` WHERE t.id = $1`, id)
	out, err := scanTaskWithRelations(row)
	if err != nil {
		return TaskWithRelations{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TaskWithRelations{}, fmt.Errorf("commit set archived: %w", err)
	}

	slog.Info("task archived flag changed", "task_id", id, "number", number, "actor_id", actor.UserID, "archived", archived)
	return out, nil
}

// --- helpers ---

func validateParentAssignment(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	parentID *uuid.UUID,
	projectID uuid.UUID,
	milestoneID *uuid.UUID,
	status domain.TaskStatus,
) error {
	if parentID == nil {
		return nil
	}
	if *parentID == taskID {
		return fmt.Errorf("%w: a task cannot be its own parent", domain.ErrInvalidInput)
	}

	var (
		parentParentID  *uuid.UUID
		parentProjectID uuid.UUID
		parentMilestone *uuid.UUID
		parentStatus    domain.TaskStatus
		parentArchived  bool
	)
	err := tx.QueryRow(ctx, `
		SELECT parent_task_id, project_id, milestone_id, status, archived_at IS NOT NULL
		FROM tasks
		WHERE id=$1
		FOR SHARE`,
		*parentID,
	).Scan(
		&parentParentID,
		&parentProjectID,
		&parentMilestone,
		&parentStatus,
		&parentArchived,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read task parent: %w", err)
	}
	if parentArchived {
		return fmt.Errorf("%w: an archived task cannot become a parent", domain.ErrConflict)
	}
	if parentParentID != nil {
		return fmt.Errorf("%w: task relationships support exactly one parent-child level", domain.ErrConflict)
	}
	if parentProjectID != projectID || !uuidPtrsEqual(parentMilestone, milestoneID) {
		return fmt.Errorf("%w: parent and child must share project and milestone", domain.ErrConflict)
	}
	var hasChildren bool
	if err := tx.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM tasks WHERE parent_task_id=$1)`,
		taskID,
	).Scan(&hasChildren); err != nil {
		return fmt.Errorf("check task children: %w", err)
	}
	if hasChildren {
		return fmt.Errorf("%w: a task with children cannot become a child", domain.ErrConflict)
	}
	if parentStatus == domain.TaskStatusDone &&
		status != domain.TaskStatusDone &&
		status != domain.TaskStatusCancelled {
		return fmt.Errorf("%w: a completed parent cannot receive an unfinished child", domain.ErrConflict)
	}
	var directlyRelated bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM task_dependencies
			WHERE (task_id=$1 AND depends_on_task_id=$2)
			   OR (task_id=$2 AND depends_on_task_id=$1)
		)`,
		taskID,
		*parentID,
	).Scan(&directlyRelated); err != nil {
		return fmt.Errorf("check parent dependency overlap: %w", err)
	}
	if directlyRelated {
		return fmt.Errorf("%w: direct parent and child cannot also be dependencies", domain.ErrConflict)
	}
	return nil
}

func validateDependencyAssignments(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	projectID uuid.UUID,
	parentID *uuid.UUID,
	dependencyIDs []uuid.UUID,
) error {
	seen := make(map[uuid.UUID]struct{}, len(dependencyIDs))
	for _, dependencyID := range dependencyIDs {
		if dependencyID == taskID {
			return fmt.Errorf("%w: a task cannot depend on itself", domain.ErrInvalidInput)
		}
		if _, duplicate := seen[dependencyID]; duplicate {
			return fmt.Errorf("%w: duplicate task dependency", domain.ErrInvalidInput)
		}
		seen[dependencyID] = struct{}{}

		var (
			dependencyProject  uuid.UUID
			dependencyParent   *uuid.UUID
			dependencyArchived bool
		)
		err := tx.QueryRow(ctx, `
			SELECT project_id, parent_task_id, archived_at IS NOT NULL
			FROM tasks
			WHERE id=$1
			FOR SHARE`,
			dependencyID,
		).Scan(&dependencyProject, &dependencyParent, &dependencyArchived)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read task dependency: %w", err)
		}
		if dependencyArchived {
			return fmt.Errorf("%w: archived tasks cannot be dependencies", domain.ErrConflict)
		}
		if dependencyProject != projectID {
			return fmt.Errorf("%w: task dependencies must stay within one project", domain.ErrConflict)
		}
		if (parentID != nil && *parentID == dependencyID) ||
			(dependencyParent != nil && *dependencyParent == taskID) {
			return fmt.Errorf("%w: direct parent and child cannot also be dependencies", domain.ErrConflict)
		}

		var createsCycle bool
		if err := tx.QueryRow(ctx, `
			WITH RECURSIVE reachable(id) AS (
				SELECT depends_on_task_id
				FROM task_dependencies
				WHERE task_id=$1
				UNION
				SELECT dependency.depends_on_task_id
				FROM task_dependencies dependency
				JOIN reachable ON reachable.id=dependency.task_id
			)
			SELECT EXISTS (SELECT 1 FROM reachable WHERE id=$2)`,
			dependencyID,
			taskID,
		).Scan(&createsCycle); err != nil {
			return fmt.Errorf("check task dependency cycle: %w", err)
		}
		if createsCycle {
			return fmt.Errorf("%w: task dependency cycle is not allowed", domain.ErrConflict)
		}
	}
	return nil
}

func validateExistingDependenciesForProject(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	projectID uuid.UUID,
) error {
	var invalid bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM task_dependencies dependency
			JOIN tasks related
			  ON related.id=CASE
				WHEN dependency.task_id=$1 THEN dependency.depends_on_task_id
				ELSE dependency.task_id
			  END
			WHERE (dependency.task_id=$1 OR dependency.depends_on_task_id=$1)
			  AND related.project_id<>$2
		)`,
		taskID,
		projectID,
	).Scan(&invalid); err != nil {
		return fmt.Errorf("validate dependency project: %w", err)
	}
	if invalid {
		return fmt.Errorf("%w: detach dependencies before moving this task to another project", domain.ErrConflict)
	}
	return nil
}

func replaceTaskDependencies(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	dependencyIDs []uuid.UUID,
) error {
	if _, err := tx.Exec(ctx, `DELETE FROM task_dependencies WHERE task_id=$1`, taskID); err != nil {
		return fmt.Errorf("clear task dependencies: %w", err)
	}
	for _, dependencyID := range dependencyIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_dependencies (task_id, depends_on_task_id)
			VALUES ($1,$2)`,
			taskID,
			dependencyID,
		); err != nil {
			return mapPgError(err)
		}
	}
	return nil
}

func taskDependencyIDs(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	lock bool,
) ([]uuid.UUID, error) {
	query := `SELECT depends_on_task_id FROM task_dependencies WHERE task_id=$1 ORDER BY depends_on_task_id`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task dependency IDs: %w", err)
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan task dependency ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list task dependency IDs: %w", err)
	}
	return ids, nil
}

func countUnfinishedTasks(
	ctx context.Context,
	tx pgx.Tx,
	taskIDs []uuid.UUID,
) (int, error) {
	if len(taskIDs) == 0 {
		return 0, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM tasks
		WHERE id=ANY($1::uuid[])
		  AND (phase IS NULL OR phase NOT IN ('done', 'cancelled'))`,
		taskIDs,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unfinished tasks: %w", err)
	}
	return count, nil
}

func moveTaskChildren(
	ctx context.Context,
	tx pgx.Tx,
	parentID uuid.UUID,
	projectID uuid.UUID,
	milestoneID *uuid.UUID,
	actor domain.OperationActor,
) error {
	rows, err := tx.Query(ctx, `
		SELECT id, number, project_id, milestone_id
		FROM tasks
		WHERE parent_task_id=$1
		ORDER BY number
		FOR UPDATE`,
		parentID,
	)
	if err != nil {
		return fmt.Errorf("lock task children for move: %w", err)
	}
	type childMove struct {
		id           uuid.UUID
		number       int64
		oldProject   uuid.UUID
		oldMilestone *uuid.UUID
	}
	children := []childMove{}
	groupIDs := []uuid.UUID{parentID}
	for rows.Next() {
		var child childMove
		if err := rows.Scan(
			&child.id,
			&child.number,
			&child.oldProject,
			&child.oldMilestone,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan task child for move: %w", err)
		}
		children = append(children, child)
		groupIDs = append(groupIDs, child.id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list task children for move: %w", err)
	}
	if len(children) == 0 {
		return nil
	}

	if children[0].oldProject != projectID {
		var externalDependency bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM task_dependencies
				WHERE (task_id=ANY($1::uuid[]) AND NOT (depends_on_task_id=ANY($1::uuid[])))
				   OR (depends_on_task_id=ANY($1::uuid[]) AND NOT (task_id=ANY($1::uuid[])))
			)`,
			groupIDs,
		).Scan(&externalDependency); err != nil {
			return fmt.Errorf("validate child group dependencies: %w", err)
		}
		if externalDependency {
			return fmt.Errorf(
				"%w: detach external dependencies before moving the parent group to another project",
				domain.ErrConflict,
			)
		}
	}

	for _, child := range children {
		if _, err := tx.Exec(ctx, `
			UPDATE tasks
			SET project_id=$2, milestone_id=$3, version=version+1, updated_at=now()
			WHERE id=$1`,
			child.id,
			projectID,
			milestoneID,
		); err != nil {
			return mapPgError(err)
		}
		if err := recordFieldChange(
			ctx, tx, child.id, actor, domain.ActivityFieldProject,
			child.oldProject.String(), projectID.String(),
		); err != nil {
			return err
		}
		if err := recordFieldChange(
			ctx, tx, child.id, actor, domain.ActivityFieldMilestone,
			uuidPtrString(child.oldMilestone), uuidPtrString(milestoneID),
		); err != nil {
			return err
		}
		childNumber := child.number
		oldValue, _ := json.Marshal(map[string]any{
			"project_id":   child.oldProject,
			"milestone_id": child.oldMilestone,
		})
		newValue, _ := json.Marshal(map[string]any{
			"project_id":           projectID,
			"milestone_id":         milestoneID,
			"moved_with_parent_id": parentID,
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt:   time.Now().UTC(),
			Actor:        actor,
			EntityType:   "task",
			EntityID:     child.id,
			EntityNumber: &childNumber,
			Action:       "moved_with_parent",
			OldValue:     oldValue,
			NewValue:     newValue,
		}); err != nil {
			return err
		}
	}
	return nil
}

func shiftTaskChildrenSchedules(
	ctx context.Context,
	tx pgx.Tx,
	parentID uuid.UUID,
	days int,
	actor domain.OperationActor,
) error {
	rows, err := tx.Query(ctx, `
		SELECT id, number, start_date, due_date
		FROM tasks
		WHERE parent_task_id=$1
		  AND (start_date IS NOT NULL OR due_date IS NOT NULL)
		ORDER BY number
		FOR UPDATE`,
		parentID,
	)
	if err != nil {
		return fmt.Errorf("lock child schedules: %w", err)
	}
	type scheduledChild struct {
		id        uuid.UUID
		number    int64
		startDate *time.Time
		dueDate   *time.Time
	}
	children := []scheduledChild{}
	for rows.Next() {
		var child scheduledChild
		if err := rows.Scan(
			&child.id,
			&child.number,
			&child.startDate,
			&child.dueDate,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan child schedule: %w", err)
		}
		children = append(children, child)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list child schedules: %w", err)
	}

	for _, child := range children {
		var shiftedStart, shiftedDue *time.Time
		if child.startDate != nil {
			value := child.startDate.AddDate(0, 0, days)
			shiftedStart = &value
		}
		if child.dueDate != nil {
			value := child.dueDate.AddDate(0, 0, days)
			shiftedDue = &value
		}
		if _, err := tx.Exec(ctx, `
			UPDATE tasks
			SET start_date=$2, due_date=$3, version=version+1, updated_at=now()
			WHERE id=$1`,
			child.id,
			shiftedStart,
			shiftedDue,
		); err != nil {
			return mapPgError(err)
		}
		if err := recordFieldChange(
			ctx, tx, child.id, actor, domain.ActivityFieldStartDate,
			datePtrString(child.startDate), datePtrString(shiftedStart),
		); err != nil {
			return err
		}
		if err := recordFieldChange(
			ctx, tx, child.id, actor, domain.ActivityFieldDueDate,
			datePtrString(child.dueDate), datePtrString(shiftedDue),
		); err != nil {
			return err
		}
		childNumber := child.number
		oldValue, _ := json.Marshal(map[string]any{
			"start_date": child.startDate,
			"due_date":   child.dueDate,
		})
		newValue, _ := json.Marshal(map[string]any{
			"start_date":             shiftedStart,
			"due_date":               shiftedDue,
			"shifted_with_parent_id": parentID,
			"shift_days":             days,
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt:   time.Now().UTC(),
			Actor:        actor,
			EntityType:   "task",
			EntityID:     child.id,
			EntityNumber: &childNumber,
			Action:       "schedule_shifted_with_parent",
			OldValue:     oldValue,
			NewValue:     newValue,
		}); err != nil {
			return err
		}
	}
	return nil
}

func uuidPtrsEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func uuidListString(ids []uuid.UUID) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = id.String()
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

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

func insertActivity(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	actor domain.OperationActor,
	field domain.ActivityField,
	oldValue, newValue *string,
) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO task_activity (
			id, task_id, actor_id, field, old_value, new_value,
			request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), taskID, actor.UserID, string(field), oldValue, newValue,
		actor.RequestID, actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName),
		actor.AgentRunID)
	if err != nil {
		return fmt.Errorf("insert activity %s for task %s: %w", field, taskID, err)
	}
	return nil
}

// recordFieldChange writes an activity entry only when oldVal != newVal,
// keeping the log free of no-op noise from a PATCH that resends a field's
// current value unchanged.
func recordFieldChange(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	actor domain.OperationActor,
	field domain.ActivityField,
	oldVal, newVal string,
) error {
	if oldVal == newVal {
		return nil
	}
	return insertActivity(ctx, tx, taskID, actor, field, strPtrOrNil(oldVal), strPtrOrNil(newVal))
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
		`SELECT id, task_id, actor_id, field, old_value, new_value,
		        request_id, auth_method, api_token_id, token_name_snapshot,
		        agent_run_id, created_at
		 FROM task_activity WHERE task_id = $1 ORDER BY created_at ASC, id ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query activity for task %s: %w", taskID, err)
	}
	defer rows.Close()

	out := []domain.Activity{}
	for rows.Next() {
		var a domain.Activity
		if err := rows.Scan(
			&a.ID, &a.TaskID, &a.ActorID, &a.Field, &a.OldValue, &a.NewValue,
			&a.RequestID, &a.AuthMethod, &a.APITokenID, &a.TokenName,
			&a.AgentRunID, &a.CreatedAt,
		); err != nil {
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
	Phases     []domain.TaskPhase
	Activities []domain.TaskActivityState
	// ClaimableStage applies the lifecycle's stage-aware availability rule.
	// Execution availability cannot be represented by independent phase and
	// activity predicates because it includes ready Tasks without an activity.
	ClaimableStage domain.TaskClaimStage
	// Statuses and ExecutionModes are retained only for pre-migration internal callers.
	Statuses       []domain.TaskStatus
	Priorities     []domain.TaskPriority
	ExecutionModes []domain.TaskExecutionMode

	// AssigneeID filters to tasks assigned to one user. Unassigned, if true,
	// overrides AssigneeID and filters to tasks with no assignee instead —
	// unassigned must be a first-class, filterable state, not an omission.
	AssigneeID *uuid.UUID
	Unassigned bool

	LabelIDs    []uuid.UUID
	Search      string
	ProjectID   *uuid.UUID
	MilestoneID *uuid.UUID
	CreatorID   *uuid.UUID
	BacklogOnly bool
	// VisibleToUserID restricts results to Project memberships. Nil is reserved
	// for trusted internal callers and the platform Administrator override.
	VisibleToUserID *uuid.UUID

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
	if len(f.Phases) > 0 {
		where = append(where, "t.phase = ANY("+arg(phaseStrings(f.Phases))+"::text[])")
	}
	if len(f.Activities) > 0 {
		where = append(where, "t.activity_state = ANY("+arg(activityStrings(f.Activities))+"::text[])")
	}
	switch f.ClaimableStage {
	case "":
	case domain.TaskClaimStageExecution:
		where = append(where, "(t.phase='ready' OR (t.phase='in_progress' AND t.activity_state='available'))")
	case domain.TaskClaimStageReview:
		where = append(where, "t.phase='in_review' AND t.activity_state='available'")
	default:
		return TaskListResult{}, fmt.Errorf(
			"%w: invalid claimable Task stage %q", domain.ErrInvalidInput, f.ClaimableStage,
		)
	}
	if len(f.Priorities) > 0 {
		where = append(where, "t.priority = ANY("+arg(priorityStrings(f.Priorities))+"::text[])")
	}
	if len(f.ExecutionModes) > 0 {
		where = append(
			where,
			"t.execution_mode = ANY("+arg(executionModeStrings(f.ExecutionModes))+"::text[])",
		)
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
	if f.MilestoneID != nil {
		where = append(where, "t.milestone_id = "+arg(*f.MilestoneID))
	}
	if f.CreatorID != nil {
		where = append(where, "t.creator_id = "+arg(*f.CreatorID))
	}
	if f.BacklogOnly {
		where = append(where, "t.milestone_id IS NULL")
	}
	if f.VisibleToUserID != nil {
		where = append(where, "EXISTS (SELECT 1 FROM project_memberships pm WHERE pm.project_id=t.project_id AND pm.user_id="+arg(*f.VisibleToUserID)+")")
	}
	if strings.TrimSpace(f.Search) != "" {
		needle := "%" + escapeLike(f.Search) + "%"
		placeholder := arg(needle)
		where = append(where, fmt.Sprintf(
			"(t.title ILIKE %[1]s ESCAPE '\\' OR t.context ILIKE %[1]s ESCAPE '\\' OR "+
				"t.expected_result ILIKE %[1]s ESCAPE '\\' OR t.description ILIKE %[1]s ESCAPE '\\')",
			placeholder,
		))
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

func phaseStrings(values []domain.TaskPhase) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func activityStrings(values []domain.TaskActivityState) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
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

func executionModeStrings(modes []domain.TaskExecutionMode) []string {
	out := make([]string, len(modes))
	for i, mode := range modes {
		out[i] = string(mode)
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
