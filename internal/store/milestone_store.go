package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MilestoneStore struct{ db *DB }

func NewMilestoneStore(db *DB) *MilestoneStore { return &MilestoneStore{db: db} }

func scanMilestone(s scanner) (domain.Milestone, error) {
	var milestone domain.Milestone
	err := s.Scan(
		&milestone.ID, &milestone.ProjectID, &milestone.Name, &milestone.Outcome,
		&milestone.Description, &milestone.Status, &milestone.TargetDate,
		&milestone.Position, &milestone.CompletedAt, &milestone.CancelledAt,
		&milestone.CreatedAt, &milestone.UpdatedAt,
	)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("scan milestone: %w", err)
	}
	return milestone, nil
}

const milestoneColumns = `id, project_id, name, outcome, description, status,
	target_date, position, completed_at, cancelled_at, created_at, updated_at`

func (s *MilestoneStore) Create(ctx context.Context, milestone domain.Milestone) (domain.Milestone, error) {
	return s.create(ctx, milestone, nil)
}

func (s *MilestoneStore) CreateWithOperation(
	ctx context.Context,
	milestone domain.Milestone,
	actor domain.OperationActor,
) (domain.Milestone, error) {
	if err := actor.Validate(); err != nil {
		return domain.Milestone{}, err
	}
	return s.create(ctx, milestone, &actor)
}

func (s *MilestoneStore) create(
	ctx context.Context,
	milestone domain.Milestone,
	actor *domain.OperationActor,
) (domain.Milestone, error) {
	if milestone.ID == uuid.Nil {
		milestone.ID = uuid.New()
	}
	if milestone.Status == "" {
		milestone.Status = domain.MilestoneStatusOpen
	}
	if err := milestone.Validate(); err != nil {
		return domain.Milestone{}, err
	}
	if actor == nil {
		out, err := scanMilestone(s.db.Pool.QueryRow(ctx, `
			INSERT INTO milestones
				(id, project_id, name, outcome, description, status, target_date, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING `+milestoneColumns,
			milestone.ID, milestone.ProjectID, milestone.Name, milestone.Outcome,
			milestone.Description, milestone.Status, milestone.TargetDate, milestone.Position))
		if err != nil {
			return domain.Milestone{}, mapPgError(err)
		}
		return out, nil
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("begin create milestone: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	out, err := scanMilestone(tx.QueryRow(ctx, `
		INSERT INTO milestones
			(id, project_id, name, outcome, description, status, target_date, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+milestoneColumns,
		milestone.ID, milestone.ProjectID, milestone.Name, milestone.Outcome,
		milestone.Description, milestone.Status, milestone.TargetDate, milestone.Position))
	if err != nil {
		return domain.Milestone{}, mapPgError(err)
	}
	newValue, _ := json.Marshal(map[string]any{
		"project_id": out.ProjectID, "name": out.Name, "outcome": out.Outcome,
		"description": out.Description, "status": out.Status,
		"target_date": out.TargetDate, "position": out.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: *actor, EntityType: "milestone",
		EntityID: out.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.Milestone{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Milestone{}, fmt.Errorf("commit create milestone: %w", err)
	}
	return out, nil
}

func (s *MilestoneStore) Get(ctx context.Context, projectID, milestoneID uuid.UUID) (domain.Milestone, error) {
	out, err := scanMilestone(s.db.Pool.QueryRow(ctx,
		`SELECT `+milestoneColumns+` FROM milestones WHERE project_id=$1 AND id=$2`,
		projectID, milestoneID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Milestone{}, domain.ErrNotFound
	}
	return out, err
}

func (s *MilestoneStore) List(ctx context.Context, projectID uuid.UUID) ([]domain.Milestone, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+milestoneColumns+` FROM milestones WHERE project_id=$1 ORDER BY position, created_at, id`,
		projectID)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	defer rows.Close()
	out := []domain.Milestone{}
	for rows.Next() {
		milestone, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, milestone)
	}
	return out, rows.Err()
}

type MilestonePatch struct {
	Name          *string
	Outcome       *string
	Description   *string
	TargetDateSet bool
	TargetDate    *time.Time
	Position      *int
}

func (s *MilestoneStore) Update(
	ctx context.Context,
	projectID, milestoneID uuid.UUID,
	patch MilestonePatch,
	actorID uuid.UUID,
) (domain.Milestone, error) {
	return s.UpdateWithOperation(
		ctx, projectID, milestoneID, patch, domain.SessionOperation(actorID, "internal"),
	)
}

func (s *MilestoneStore) UpdateWithOperation(
	ctx context.Context,
	projectID, milestoneID uuid.UUID,
	patch MilestonePatch,
	actor domain.OperationActor,
) (domain.Milestone, error) {
	if err := actor.Validate(); err != nil {
		return domain.Milestone{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("begin update milestone: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanMilestone(tx.QueryRow(ctx,
		`SELECT `+milestoneColumns+`
		 FROM milestones WHERE project_id=$1 AND id=$2 FOR UPDATE`,
		projectID, milestoneID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Milestone{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Milestone{}, err
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
	if patch.TargetDateSet {
		current.TargetDate = patch.TargetDate
	}
	if patch.Position != nil {
		current.Position = *patch.Position
	}
	if err := current.Validate(); err != nil {
		return domain.Milestone{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE milestones SET name=$3, outcome=$4, description=$5,
			target_date=$6, position=$7, updated_at=now()
		WHERE project_id=$1 AND id=$2`,
		projectID, milestoneID, current.Name, current.Outcome, current.Description,
		current.TargetDate, current.Position)
	if err != nil {
		return domain.Milestone{}, mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Milestone{}, domain.ErrNotFound
	}
	if old.Name != current.Name || old.Outcome != current.Outcome ||
		old.Description != current.Description ||
		datePtrString(old.TargetDate) != datePtrString(current.TargetDate) ||
		old.Position != current.Position {
		_, err = tx.Exec(ctx, `
			INSERT INTO project_activity
				(id, project_id, milestone_id, actor_id, action, old_value, new_value,
				 request_id, auth_method, api_token_id, token_name_snapshot)
			VALUES ($1,$2,$3,$4,'milestone_updated',$5,$6,$7,$8,$9,$10)`,
			uuid.New(), projectID, milestoneID, actor.UserID, old.Name, current.Name,
			actor.RequestID, actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName))
		if err != nil {
			return domain.Milestone{}, fmt.Errorf("record milestone update: %w", err)
		}
	}
	oldValue, _ := json.Marshal(map[string]any{
		"name": old.Name, "outcome": old.Outcome, "description": old.Description,
		"target_date": old.TargetDate, "position": old.Position,
	})
	newValue, _ := json.Marshal(map[string]any{
		"name": current.Name, "outcome": current.Outcome, "description": current.Description,
		"target_date": current.TargetDate, "position": current.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
		EntityID: current.ID, Action: "updated", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.Milestone{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Milestone{}, fmt.Errorf("commit update milestone: %w", err)
	}
	return s.Get(ctx, projectID, milestoneID)
}

type MilestoneLifecycleAction string

const (
	MilestoneActionComplete MilestoneLifecycleAction = "milestone_completed"
	MilestoneActionCancel   MilestoneLifecycleAction = "milestone_cancelled"
	MilestoneActionReopen   MilestoneLifecycleAction = "milestone_reopened"
)

func (s *MilestoneStore) ApplyLifecycle(
	ctx context.Context,
	projectID, milestoneID uuid.UUID,
	action MilestoneLifecycleAction,
	actor domain.Actor,
	reason string,
) (domain.Milestone, error) {
	if !actor.IsHuman() || actor.UserID == nil {
		return domain.Milestone{}, fmt.Errorf("%w: milestone lifecycle actions require a human user", domain.ErrForbidden)
	}
	return s.ApplyLifecycleWithOperation(
		ctx, projectID, milestoneID, action,
		domain.SessionOperation(*actor.UserID, "internal"), reason,
	)
}

func (s *MilestoneStore) ApplyLifecycleWithOperation(
	ctx context.Context,
	projectID, milestoneID uuid.UUID,
	action MilestoneLifecycleAction,
	actor domain.OperationActor,
	reason string,
) (domain.Milestone, error) {
	if err := actor.Validate(); err != nil {
		return domain.Milestone{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("begin milestone lifecycle: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	milestone, err := scanMilestone(tx.QueryRow(ctx,
		`SELECT `+milestoneColumns+` FROM milestones WHERE project_id=$1 AND id=$2 FOR UPDATE`,
		projectID, milestoneID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Milestone{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Milestone{}, err
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
			(SELECT count(*) FROM tasks t WHERE t.milestone_id=$1 AND t.status NOT IN ('done','cancelled'))`,
		milestone.ID,
	).Scan(&readiness.ActiveCriteria, &readiness.UnsatisfiedCriteria, &readiness.UnfinishedTasks)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("read milestone readiness: %w", err)
	}
	oldStatus := milestone.Status
	switch action {
	case MilestoneActionComplete:
		err = milestone.Complete(readiness)
	case MilestoneActionCancel:
		err = milestone.Cancel(readiness)
	case MilestoneActionReopen:
		userID := actor.UserID
		err = milestone.Reopen(domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, reason)
	default:
		err = fmt.Errorf("%w: unknown milestone lifecycle action", domain.ErrInvalidInput)
	}
	if err != nil {
		return domain.Milestone{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE milestones SET status=$3, completed_at=$4, cancelled_at=$5, updated_at=$6
		WHERE project_id=$1 AND id=$2`,
		projectID, milestoneID, milestone.Status, milestone.CompletedAt, milestone.CancelledAt, milestone.UpdatedAt)
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("update milestone lifecycle: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, milestone_id, actor_id, action, reason, old_value, new_value,
			 request_id, auth_method, api_token_id, token_name_snapshot)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		uuid.New(), projectID, milestoneID, actor.UserID, action, reason, oldStatus, milestone.Status,
		actor.RequestID, actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName))
	if err != nil {
		return domain.Milestone{}, fmt.Errorf("record milestone lifecycle: %w", err)
	}
	oldValue, _ := json.Marshal(map[string]any{"status": oldStatus})
	newValue, _ := json.Marshal(map[string]any{"status": milestone.Status, "reason": reason})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
		EntityID: milestone.ID, Action: string(action),
		OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.Milestone{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Milestone{}, fmt.Errorf("commit milestone lifecycle: %w", err)
	}
	return milestone, nil
}

func (s *MilestoneStore) BelongsToProject(ctx context.Context, milestoneID, projectID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM milestones WHERE id=$1 AND project_id=$2)`,
		milestoneID, projectID).Scan(&exists)
	return exists, err
}

func dateAtMidnight(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
