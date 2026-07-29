package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProjectStore struct{ db *DB }

func NewProjectStore(db *DB) *ProjectStore { return &ProjectStore{db: db} }

type ProjectWithRelations struct {
	Project        domain.Project
	Owner          domain.UserRef
	Creator        domain.UserRef
	CompletedTasks int
	EligibleTasks  int
}

type ProjectPatch struct {
	Name        *string
	Description *string
	OwnerID     *uuid.UUID
}

const projectSelectColumns = `p.id, p.number, p.version, p.name, p.description,
	p.owner_id, p.creator_id, p.archived_at, p.created_at, p.updated_at,
	ou.id, ou.name, ou.email, cu.id, cu.name, cu.email,
	(SELECT count(*) FROM tasks t WHERE t.project_id = p.id AND t.archived_at IS NULL AND t.status = 'done'),
	(SELECT count(*) FROM tasks t WHERE t.project_id = p.id AND t.archived_at IS NULL AND t.status <> 'cancelled')`

const projectFromJoins = `FROM projects p
	JOIN users ou ON ou.id = p.owner_id
	JOIN users cu ON cu.id = p.creator_id`

func scanProject(s scanner) (ProjectWithRelations, error) {
	var out ProjectWithRelations
	if err := s.Scan(
		&out.Project.ID, &out.Project.Number, &out.Project.Version,
		&out.Project.Name, &out.Project.Description,
		&out.Project.OwnerID, &out.Project.CreatorID, &out.Project.ArchivedAt,
		&out.Project.CreatedAt, &out.Project.UpdatedAt,
		&out.Owner.ID, &out.Owner.Name, &out.Owner.Email,
		&out.Creator.ID, &out.Creator.Name, &out.Creator.Email,
		&out.CompletedTasks, &out.EligibleTasks,
	); err != nil {
		return ProjectWithRelations{}, fmt.Errorf("scan project: %w", err)
	}
	return out, nil
}

func (s *ProjectStore) Create(ctx context.Context, project domain.Project) (ProjectWithRelations, error) {
	return s.CreateWithOperation(
		ctx, project, domain.SessionOperation(project.CreatorID, "internal"),
	)
}

func (s *ProjectStore) CreateWithOperation(
	ctx context.Context,
	project domain.Project,
	actor domain.OperationActor,
) (ProjectWithRelations, error) {
	if err := actor.Validate(); err != nil {
		return ProjectWithRelations{}, err
	}
	if project.CreatorID != actor.UserID {
		return ProjectWithRelations{}, fmt.Errorf("%w: project creator must match operation actor", domain.ErrForbidden)
	}
	if project.ID == uuid.Nil {
		project.ID = uuid.New()
	}
	if err := project.Validate(); err != nil {
		return ProjectWithRelations{}, err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("begin create project: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row := tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO projects
				(id, name, description, owner_id, creator_id)
			VALUES ($1,$2,$3,$4,$5)
			RETURNING *
		)
		SELECT `+projectSelectColumns+`
		FROM inserted p
		JOIN users ou ON ou.id = p.owner_id
		JOIN users cu ON cu.id = p.creator_id`,
		project.ID, project.Name, project.Description, project.OwnerID, project.CreatorID,
	)
	out, err := scanProject(row)
	if err != nil {
		return ProjectWithRelations{}, mapPgError(err)
	}
	newValue, _ := json.Marshal(map[string]any{
		"name": project.Name, "description": project.Description, "owner_id": project.OwnerID,
	})
	number := out.Project.Number
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "project",
		EntityID: project.ID, EntityNumber: &number, Action: "created",
		NewValue: newValue,
	}); err != nil {
		return ProjectWithRelations{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectWithRelations{}, fmt.Errorf("commit create project: %w", err)
	}
	return out, nil
}

func (s *ProjectStore) GetByNumber(ctx context.Context, number int64) (ProjectWithRelations, error) {
	out, err := scanProject(s.db.Pool.QueryRow(ctx,
		`SELECT `+projectSelectColumns+` `+projectFromJoins+` WHERE p.number = $1`, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectWithRelations{}, domain.ErrNotFound
	}
	return out, err
}

func (s *ProjectStore) List(ctx context.Context, includeArchived bool) ([]ProjectWithRelations, error) {
	archivedClause := "WHERE p.archived_at IS NULL"
	if includeArchived {
		archivedClause = ""
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT `+projectSelectColumns+` `+projectFromJoins+` `+
		archivedClause+`
		ORDER BY p.archived_at NULLS FIRST, lower(p.name), p.number`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	out := []ProjectWithRelations{}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	return out, rows.Err()
}

func (s *ProjectStore) Update(
	ctx context.Context,
	number int64,
	patch ProjectPatch,
	actorID uuid.UUID,
) (ProjectWithRelations, error) {
	return s.UpdateWithOperation(
		ctx, number, patch, domain.SessionOperation(actorID, "internal"),
	)
}

func (s *ProjectStore) UpdateWithOperation(
	ctx context.Context,
	number int64,
	patch ProjectPatch,
	actor domain.OperationActor,
) (ProjectWithRelations, error) {
	return s.updateWithExpectedVersion(ctx, number, nil, patch, actor)
}

func (s *ProjectStore) UpdateVersionedWithOperation(
	ctx context.Context,
	number, expectedVersion int64,
	patch ProjectPatch,
	actor domain.OperationActor,
) (ProjectWithRelations, error) {
	return s.updateWithExpectedVersion(ctx, number, &expectedVersion, patch, actor)
}

func (s *ProjectStore) updateWithExpectedVersion(
	ctx context.Context,
	number int64,
	expectedVersion *int64,
	patch ProjectPatch,
	actor domain.OperationActor,
) (ProjectWithRelations, error) {
	if err := actor.Validate(); err != nil {
		return ProjectWithRelations{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("begin update project: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var project domain.Project
	err = tx.QueryRow(ctx, `
		SELECT id, number, version, name, description, owner_id, creator_id,
			archived_at, created_at, updated_at
		FROM projects WHERE number=$1 FOR UPDATE`, number).
		Scan(&project.ID, &project.Number, &project.Version, &project.Name,
			&project.Description, &project.OwnerID, &project.CreatorID,
			&project.ArchivedAt, &project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("lock project %d: %w", number, err)
	}
	if expectedVersion != nil && project.Version != *expectedVersion {
		return ProjectWithRelations{}, domain.VersionConflictError{CurrentVersion: project.Version}
	}

	old := project
	if patch.Name != nil {
		project.Name = *patch.Name
	}
	if patch.Description != nil {
		project.Description = *patch.Description
	}
	if patch.OwnerID != nil {
		project.OwnerID = *patch.OwnerID
	}
	if err := project.Validate(); err != nil {
		return ProjectWithRelations{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE projects SET name=$2, description=$3, owner_id=$4,
			version=version+1, updated_at=now()
		WHERE id=$1 AND version=$5
		RETURNING version`,
		project.ID, project.Name, project.Description, project.OwnerID, project.Version,
	).Scan(&project.Version)
	if err != nil {
		return ProjectWithRelations{}, mapPgError(err)
	}

	changes := []struct {
		action string
		old    string
		new    string
	}{
		{"project_name_changed", old.Name, project.Name},
		{"project_description_changed", old.Description, project.Description},
		{"project_owner_changed", old.OwnerID.String(), project.OwnerID.String()},
	}
	for _, change := range changes {
		if change.old == change.new {
			continue
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO project_activity
				(id, project_id, actor_id, action, old_value, new_value,
				 request_id, auth_method, api_token_id, token_name_snapshot)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			uuid.New(), project.ID, actor.UserID, change.action,
			strPtrOrNil(change.old), strPtrOrNil(change.new),
			actor.RequestID, actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName))
		if err != nil {
			return ProjectWithRelations{}, fmt.Errorf("record project change: %w", err)
		}
	}
	oldValue, _ := json.Marshal(map[string]any{
		"name": old.Name, "description": old.Description, "owner_id": old.OwnerID,
	})
	newValue, _ := json.Marshal(map[string]any{
		"name": project.Name, "description": project.Description, "owner_id": project.OwnerID,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "project",
		EntityID: project.ID, EntityNumber: &number, Action: "updated",
		OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return ProjectWithRelations{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectWithRelations{}, fmt.Errorf("commit update project: %w", err)
	}
	return s.GetByNumber(ctx, number)
}

type ProjectLifecycleAction string

const (
	ProjectActionArchive ProjectLifecycleAction = "archived"
	ProjectActionRestore ProjectLifecycleAction = "restored"
)

func (s *ProjectStore) ApplyLifecycle(
	ctx context.Context,
	number int64,
	action ProjectLifecycleAction,
	actor domain.Actor,
	reason string,
) (ProjectWithRelations, error) {
	actorID := uuid.Nil
	if actor.UserID != nil {
		actorID = *actor.UserID
	}
	return s.ApplyLifecycleWithOperation(
		ctx, number, action, actor, reason,
		domain.SessionOperation(actorID, "internal"),
	)
}

func (s *ProjectStore) ApplyLifecycleWithOperation(
	ctx context.Context,
	number int64,
	action ProjectLifecycleAction,
	actor domain.Actor,
	reason string,
	operation domain.OperationActor,
) (ProjectWithRelations, error) {
	return s.applyLifecycleWithExpectedVersion(
		ctx, number, nil, action, actor, reason, operation,
	)
}

func (s *ProjectStore) ApplyLifecycleVersionedWithOperation(
	ctx context.Context,
	number, expectedVersion int64,
	action ProjectLifecycleAction,
	actor domain.Actor,
	reason string,
	operation domain.OperationActor,
) (ProjectWithRelations, error) {
	return s.applyLifecycleWithExpectedVersion(
		ctx, number, &expectedVersion, action, actor, reason, operation,
	)
}

func (s *ProjectStore) applyLifecycleWithExpectedVersion(
	ctx context.Context,
	number int64,
	expectedVersion *int64,
	action ProjectLifecycleAction,
	actor domain.Actor,
	reason string,
	operation domain.OperationActor,
) (ProjectWithRelations, error) {
	if !actor.IsHuman() {
		return ProjectWithRelations{}, fmt.Errorf("%w: Project archive actions require a human user", domain.ErrForbidden)
	}
	if err := operation.Validate(); err != nil {
		return ProjectWithRelations{}, err
	}
	if actor.UserID == nil || *actor.UserID != operation.UserID {
		return ProjectWithRelations{}, fmt.Errorf("%w: lifecycle actor does not match operation actor", domain.ErrForbidden)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("begin Project archive action: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var project domain.Project
	err = tx.QueryRow(ctx, `
		SELECT id, number, version, name, description, owner_id, creator_id,
			archived_at, created_at, updated_at
		FROM projects WHERE number=$1 FOR UPDATE`, number).
		Scan(&project.ID, &project.Number, &project.Version, &project.Name,
			&project.Description, &project.OwnerID, &project.CreatorID,
			&project.ArchivedAt, &project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectWithRelations{}, domain.ErrNotFound
	}
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("lock project %d: %w", number, err)
	}
	if expectedVersion != nil && project.Version != *expectedVersion {
		return ProjectWithRelations{}, domain.VersionConflictError{CurrentVersion: project.Version}
	}

	oldArchivedAt := project.ArchivedAt
	switch action {
	case ProjectActionArchive:
		readiness, readinessErr := projectArchiveReadiness(ctx, tx, project.ID)
		if readinessErr != nil {
			return ProjectWithRelations{}, readinessErr
		}
		if err := project.Archive(readiness); err != nil {
			return ProjectWithRelations{}, err
		}
	case ProjectActionRestore:
		project.Restore()
	default:
		return ProjectWithRelations{}, fmt.Errorf("%w: unknown Project archive action", domain.ErrInvalidInput)
	}

	err = tx.QueryRow(ctx, `
		UPDATE projects SET archived_at=$2, version=version+1, updated_at=$3
		WHERE id=$1 AND version=$4
		RETURNING version`,
		project.ID, project.ArchivedAt, project.UpdatedAt, project.Version,
	).Scan(&project.Version)
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("update Project archive state: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, actor_id, action, reason, old_value, new_value,
			 request_id, auth_method, api_token_id, token_name_snapshot)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), project.ID, operation.UserID, action, nullIfEmpty(reason),
		timePtrString(oldArchivedAt), timePtrString(project.ArchivedAt),
		operation.RequestID, operation.AuthMethod, operation.TokenID, nullIfEmpty(operation.TokenName))
	if err != nil {
		return ProjectWithRelations{}, fmt.Errorf("record Project archive action: %w", err)
	}
	oldValue, _ := json.Marshal(map[string]any{"archived_at": oldArchivedAt})
	newValue, _ := json.Marshal(map[string]any{"archived_at": project.ArchivedAt})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: operation, EntityType: "project",
		EntityID: project.ID, EntityNumber: &number, Action: string(action),
		OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return ProjectWithRelations{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectWithRelations{}, fmt.Errorf("commit Project archive action: %w", err)
	}
	return s.GetByNumber(ctx, number)
}

func projectArchiveReadiness(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
) (domain.ProjectArchiveReadiness, error) {
	var readiness domain.ProjectArchiveReadiness
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM milestones m
			 WHERE m.project_id=$1 AND m.status IN ('planned','active')),
			(SELECT count(*) FROM tasks t
			 WHERE t.project_id=$1 AND t.status NOT IN ('done','cancelled'))`,
		projectID,
	).Scan(&readiness.OpenMilestones, &readiness.UnfinishedTasks)
	if err != nil {
		return domain.ProjectArchiveReadiness{}, fmt.Errorf("read Project archive readiness: %w", err)
	}
	return readiness, nil
}

func (s *ProjectStore) ResolveProjectID(ctx context.Context, number int64) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE number=$1`, number).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve project %d: %w", number, err)
	}
	return id, nil
}

func (s *ProjectStore) ListActivity(ctx context.Context, projectID uuid.UUID) ([]domain.ProjectActivity, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, project_id, milestone_id, actor_id, action, reason,
			old_value, new_value, request_id, auth_method, api_token_id,
			token_name_snapshot, created_at
		FROM project_activity
		WHERE project_id=$1
		ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project activity: %w", err)
	}
	defer rows.Close()
	out := []domain.ProjectActivity{}
	for rows.Next() {
		var activity domain.ProjectActivity
		if err := rows.Scan(
			&activity.ID, &activity.ProjectID, &activity.MilestoneID,
			&activity.ActorID, &activity.Action, &activity.Reason,
			&activity.OldValue, &activity.NewValue, &activity.RequestID,
			&activity.AuthMethod, &activity.APITokenID, &activity.TokenName,
			&activity.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan project activity: %w", err)
		}
		out = append(out, activity)
	}
	return out, rows.Err()
}

func timePtrString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
