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

type MilestoneMutation struct {
	Milestone      domain.Milestone
	ProjectVersion int64
}

func (s *MilestoneStore) CreateVersionedWithOperation(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	milestone domain.Milestone,
	actor domain.OperationActor,
) (MilestoneMutation, error) {
	if err := actor.Validate(); err != nil {
		return MilestoneMutation{}, err
	}
	if milestone.ID == uuid.Nil {
		milestone.ID = uuid.New()
	}
	milestone.ProjectID = projectID
	if milestone.Status == "" {
		milestone.Status = domain.MilestoneStatusPlanned
	}
	if err := milestone.Validate(); err != nil {
		return MilestoneMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("begin versioned milestone create: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockVersion(ctx, tx, "projects", projectID)
	if err != nil {
		return MilestoneMutation{}, err
	}
	if projectVersion != expectedProjectVersion {
		return MilestoneMutation{}, domain.VersionConflictError{CurrentVersion: projectVersion}
	}
	created, err := scanMilestone(tx.QueryRow(ctx, `
		INSERT INTO milestones
			(id, project_id, name, outcome, description, owner_id, status, target_date, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+milestoneColumns,
		milestone.ID, projectID, milestone.Name, milestone.Outcome,
		milestone.Description, milestone.OwnerID, milestone.Status, milestone.TargetDate, milestone.Position,
	))
	if err != nil {
		return MilestoneMutation{}, mapPgError(err)
	}
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return MilestoneMutation{}, err
	}
	newValue, _ := json.Marshal(map[string]any{
		"project_id": projectID, "name": created.Name, "outcome": created.Outcome,
		"description": created.Description, "owner_id": created.OwnerID, "status": created.Status,
		"target_date": created.TargetDate, "position": created.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
		EntityID: created.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return MilestoneMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MilestoneMutation{}, fmt.Errorf("commit versioned milestone create: %w", err)
	}
	return MilestoneMutation{Milestone: created, ProjectVersion: projectVersion}, nil
}

func (s *MilestoneStore) UpdateVersionedWithOperation(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	milestoneID uuid.UUID,
	expectedMilestoneVersion int64,
	patch MilestonePatch,
	actor domain.OperationActor,
) (MilestoneMutation, error) {
	if err := actor.Validate(); err != nil {
		return MilestoneMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("begin versioned milestone update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockVersion(ctx, tx, "projects", projectID)
	if err != nil {
		return MilestoneMutation{}, err
	}
	if projectVersion != expectedProjectVersion {
		return MilestoneMutation{}, domain.VersionConflictError{CurrentVersion: projectVersion}
	}
	current, err := scanMilestone(tx.QueryRow(ctx,
		`SELECT `+milestoneColumns+`
		 FROM milestones WHERE project_id=$1 AND id=$2 FOR UPDATE`,
		projectID, milestoneID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return MilestoneMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return MilestoneMutation{}, err
	}
	if current.Version != expectedMilestoneVersion {
		return MilestoneMutation{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	old := current
	if patch.Name != nil {
		current.Name = *patch.Name
	}
	if patch.Outcome != nil {
		current.Outcome = *patch.Outcome
	}
	if patch.Description != nil {
		current.Description = *patch.Description
	}
	if patch.OwnerID != nil {
		current.OwnerID = *patch.OwnerID
	}
	if patch.TargetDateSet {
		current.TargetDate = patch.TargetDate
	}
	if patch.Position != nil {
		current.Position = *patch.Position
	}
	if err := current.Validate(); err != nil {
		return MilestoneMutation{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE milestones
		SET name=$3, outcome=$4, description=$5, owner_id=$6, target_date=$7, position=$8,
			version=version+1, updated_at=now()
		WHERE project_id=$1 AND id=$2 AND version=$9
		RETURNING version, updated_at`,
		projectID, milestoneID, current.Name, current.Outcome, current.Description,
		current.OwnerID, current.TargetDate, current.Position, current.Version,
	).Scan(&current.Version, &current.UpdatedAt)
	if err != nil {
		return MilestoneMutation{}, mapPgError(err)
	}
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return MilestoneMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, milestone_id, actor_id, action, old_value, new_value,
			 request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id)
		VALUES ($1,$2,$3,$4,'milestone_updated',$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), projectID, milestoneID, actor.UserID, old.Name, current.Name,
		actor.RequestID, actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName),
		actor.AgentRunID,
	)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("record milestone update: %w", err)
	}
	oldValue, _ := json.Marshal(map[string]any{
		"name": old.Name, "outcome": old.Outcome, "description": old.Description,
		"owner_id": old.OwnerID, "target_date": old.TargetDate, "position": old.Position,
	})
	newValue, _ := json.Marshal(map[string]any{
		"name": current.Name, "outcome": current.Outcome, "description": current.Description,
		"owner_id": current.OwnerID, "target_date": current.TargetDate, "position": current.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
		EntityID: current.ID, Action: "updated", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return MilestoneMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MilestoneMutation{}, fmt.Errorf("commit versioned milestone update: %w", err)
	}
	return MilestoneMutation{Milestone: current, ProjectVersion: projectVersion}, nil
}

func (s *MilestoneStore) ApplyLifecycleVersionedWithOperation(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	milestoneID uuid.UUID,
	expectedMilestoneVersion int64,
	action MilestoneLifecycleAction,
	actor domain.OperationActor,
	reason string,
) (MilestoneMutation, error) {
	if err := actor.Validate(); err != nil {
		return MilestoneMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("begin versioned milestone lifecycle: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockVersion(ctx, tx, "projects", projectID)
	if err != nil {
		return MilestoneMutation{}, err
	}
	if projectVersion != expectedProjectVersion {
		return MilestoneMutation{}, domain.VersionConflictError{CurrentVersion: projectVersion}
	}
	milestone, err := scanMilestone(tx.QueryRow(ctx,
		`SELECT `+milestoneColumns+`
		 FROM milestones WHERE project_id=$1 AND id=$2 FOR UPDATE`,
		projectID, milestoneID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return MilestoneMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return MilestoneMutation{}, err
	}
	if milestone.Version != expectedMilestoneVersion {
		return MilestoneMutation{}, domain.VersionConflictError{CurrentVersion: milestone.Version}
	}
	var readiness domain.MilestoneReadiness
	err = tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM acceptance_criteria ac WHERE ac.milestone_id=$1 AND ac.archived_at IS NULL),
			(SELECT count(*) FROM acceptance_criteria ac
			 WHERE ac.milestone_id=$1 AND ac.archived_at IS NULL
			   AND COALESCE((SELECT chk.outcome FROM acceptance_checks chk
			                 WHERE chk.criterion_id=ac.id AND chk.criterion_revision=ac.revision
			                 ORDER BY chk.checked_at DESC, chk.id DESC LIMIT 1), '') NOT IN ('passed','waived')),
			(SELECT count(*) FROM tasks t WHERE t.milestone_id=$1 AND (t.phase IS NULL OR t.phase NOT IN ('done','cancelled')))`,
		milestone.ID,
	).Scan(&readiness.ActiveCriteria, &readiness.UnsatisfiedCriteria, &readiness.UnfinishedTasks)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("read milestone readiness: %w", err)
	}
	oldStatus := milestone.Status
	switch action {
	case MilestoneActionActivate:
		err = milestone.Activate(readiness)
	case MilestoneActionComplete:
		err = milestone.Complete(readiness)
	case MilestoneActionCancel:
		err = milestone.Cancel(readiness)
	case MilestoneActionReopen:
		userID := actor.UserID
		err = milestone.Reopen(
			domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
			reason,
		)
	default:
		err = fmt.Errorf("%w: unknown milestone lifecycle action", domain.ErrInvalidInput)
	}
	if err != nil {
		return MilestoneMutation{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE milestones
		SET status=$3, completed_at=$4, cancelled_at=$5,
			version=version+1, updated_at=$6
		WHERE project_id=$1 AND id=$2 AND version=$7
		RETURNING version`,
		projectID, milestoneID, milestone.Status, milestone.CompletedAt,
		milestone.CancelledAt, milestone.UpdatedAt, milestone.Version,
	).Scan(&milestone.Version)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("update milestone lifecycle: %w", err)
	}
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return MilestoneMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, milestone_id, actor_id, action, reason, old_value, new_value,
			 request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		uuid.New(), projectID, milestoneID, actor.UserID, action, reason, oldStatus,
		milestone.Status, actor.RequestID, actor.AuthMethod, actor.TokenID,
		nullIfEmpty(actor.TokenName), actor.AgentRunID,
	)
	if err != nil {
		return MilestoneMutation{}, fmt.Errorf("record milestone lifecycle: %w", err)
	}
	oldValue, _ := json.Marshal(map[string]any{"status": oldStatus})
	newValue, _ := json.Marshal(map[string]any{"status": milestone.Status, "reason": reason})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
		EntityID: milestone.ID, Action: string(action),
		OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return MilestoneMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MilestoneMutation{}, fmt.Errorf("commit versioned milestone lifecycle: %w", err)
	}
	return MilestoneMutation{Milestone: milestone, ProjectVersion: projectVersion}, nil
}

func lockVersion(ctx context.Context, tx pgx.Tx, table string, id uuid.UUID) (int64, error) {
	if table != "projects" && table != "milestones" &&
		table != "acceptance_criteria" && table != "tasks" {
		return 0, fmt.Errorf("unsupported versioned table %q", table)
	}
	var version int64
	err := tx.QueryRow(ctx,
		`SELECT version FROM `+table+` WHERE id=$1 FOR UPDATE`,
		id,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock %s %s: %w", table, id, err)
	}
	return version, nil
}

func incrementVersion(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	id uuid.UUID,
	expectedVersion int64,
) (int64, error) {
	if table != "projects" && table != "milestones" &&
		table != "acceptance_criteria" && table != "tasks" {
		return 0, fmt.Errorf("unsupported versioned table %q", table)
	}
	var version int64
	err := tx.QueryRow(ctx, `
		UPDATE `+table+`
		SET version=version+1, updated_at=now()
		WHERE id=$1 AND version=$2
		RETURNING version`,
		id, expectedVersion,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.VersionConflictError{CurrentVersion: expectedVersion}
	}
	if err != nil {
		return 0, fmt.Errorf("increment %s %s version: %w", table, id, err)
	}
	return version, nil
}
