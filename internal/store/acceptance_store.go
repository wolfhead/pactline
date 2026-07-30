package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AcceptanceStore struct{ db *DB }

func NewAcceptanceStore(db *DB) *AcceptanceStore { return &AcceptanceStore{db: db} }

type CriterionWithCurrentCheck struct {
	Criterion    domain.AcceptanceCriterion
	CurrentCheck *domain.AcceptanceCheck
}

const criterionColumns = `ac.id, ac.version, ac.milestone_id, ac.task_id, ac.criterion,
	ac.verification_instructions, ac.revision, ac.position, ac.archived_at,
	ac.created_at, ac.updated_at`

const criterionReturnColumns = `id, version, milestone_id, task_id, criterion,
	verification_instructions, revision, position, archived_at, created_at, updated_at`

func scanCriterion(s scanner) (domain.AcceptanceCriterion, error) {
	var criterion domain.AcceptanceCriterion
	err := s.Scan(
		&criterion.ID, &criterion.Version,
		&criterion.MilestoneID, &criterion.TaskID,
		&criterion.Criterion, &criterion.VerificationInstructions,
		&criterion.Revision, &criterion.Position, &criterion.ArchivedAt,
		&criterion.CreatedAt, &criterion.UpdatedAt,
	)
	if err != nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("scan acceptance criterion: %w", err)
	}
	return criterion, nil
}

func (s *AcceptanceStore) Create(ctx context.Context, criterion domain.AcceptanceCriterion) (domain.AcceptanceCriterion, error) {
	return s.create(ctx, criterion, nil)
}

func (s *AcceptanceStore) CreateWithOperation(
	ctx context.Context,
	criterion domain.AcceptanceCriterion,
	actor domain.OperationActor,
) (domain.AcceptanceCriterion, error) {
	if err := actor.Validate(); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	return s.create(ctx, criterion, &actor)
}

func (s *AcceptanceStore) create(
	ctx context.Context,
	criterion domain.AcceptanceCriterion,
	actor *domain.OperationActor,
) (domain.AcceptanceCriterion, error) {
	if criterion.ID == uuid.Nil {
		criterion.ID = uuid.New()
	}
	if criterion.Revision == 0 {
		criterion.Revision = 1
	}
	if err := criterion.Validate(); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	if actor == nil {
		out, err := scanCriterion(s.db.Pool.QueryRow(ctx, `
			INSERT INTO acceptance_criteria
				(id, milestone_id, task_id, criterion, verification_instructions, revision, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			RETURNING `+criterionReturnColumns,
			criterion.ID, criterion.MilestoneID, criterion.TaskID, criterion.Criterion,
			criterion.VerificationInstructions, criterion.Revision, criterion.Position))
		if err != nil {
			return domain.AcceptanceCriterion{}, mapPgError(err)
		}
		return out, nil
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("begin create acceptance criterion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	out, err := scanCriterion(tx.QueryRow(ctx, `
		INSERT INTO acceptance_criteria
			(id, milestone_id, task_id, criterion, verification_instructions, revision, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+criterionReturnColumns,
		criterion.ID, criterion.MilestoneID, criterion.TaskID, criterion.Criterion,
		criterion.VerificationInstructions, criterion.Revision, criterion.Position))
	if err != nil {
		return domain.AcceptanceCriterion{}, mapPgError(err)
	}
	newValue, _ := json.Marshal(map[string]any{
		"milestone_id": out.MilestoneID, "task_id": out.TaskID,
		"criterion": out.Criterion, "verification_instructions": out.VerificationInstructions,
		"revision": out.Revision, "position": out.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: *actor, EntityType: "acceptance_criterion",
		EntityID: out.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("commit create acceptance criterion: %w", err)
	}
	return out, nil
}

func (s *AcceptanceStore) Get(ctx context.Context, criterionID uuid.UUID) (domain.AcceptanceCriterion, error) {
	out, err := scanCriterion(s.db.Pool.QueryRow(ctx,
		`SELECT `+criterionColumns+` FROM acceptance_criteria ac WHERE ac.id=$1`, criterionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptanceCriterion{}, domain.ErrNotFound
	}
	return out, err
}

func (s *AcceptanceStore) ListForMilestone(ctx context.Context, milestoneID uuid.UUID) ([]CriterionWithCurrentCheck, error) {
	return s.list(ctx, `ac.milestone_id=$1`, milestoneID)
}

func (s *AcceptanceStore) ListForTask(ctx context.Context, taskID uuid.UUID) ([]CriterionWithCurrentCheck, error) {
	return s.list(ctx, `ac.task_id=$1`, taskID)
}

func (s *AcceptanceStore) list(ctx context.Context, predicate string, ownerID uuid.UUID) ([]CriterionWithCurrentCheck, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+criterionColumns+`,
			ch.id, ch.criterion_revision, ch.outcome, ch.evidence,
			ch.checker_type, ch.checked_by_user_id, ch.checker_ref, ch.checked_at
		FROM acceptance_criteria ac
		LEFT JOIN LATERAL (
			SELECT *
			FROM acceptance_checks candidate
			WHERE candidate.criterion_id=ac.id AND candidate.criterion_revision=ac.revision
			ORDER BY candidate.checked_at DESC, candidate.id DESC
			LIMIT 1
		) ch ON true
		WHERE `+predicate+` AND ac.archived_at IS NULL
		ORDER BY ac.position, ac.created_at, ac.id`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list acceptance criteria: %w", err)
	}
	defer rows.Close()
	out := []CriterionWithCurrentCheck{}
	for rows.Next() {
		var (
			item          CriterionWithCurrentCheck
			checkID       *uuid.UUID
			revision      *int
			outcome       *domain.AcceptanceOutcome
			evidence      *string
			checkerType   *domain.ActorType
			checkerUserID *uuid.UUID
			checkerRef    *string
			checkedAt     *time.Time
		)
		err := rows.Scan(
			&item.Criterion.ID, &item.Criterion.Version,
			&item.Criterion.MilestoneID, &item.Criterion.TaskID,
			&item.Criterion.Criterion, &item.Criterion.VerificationInstructions,
			&item.Criterion.Revision, &item.Criterion.Position, &item.Criterion.ArchivedAt,
			&item.Criterion.CreatedAt, &item.Criterion.UpdatedAt,
			&checkID, &revision, &outcome, &evidence, &checkerType,
			&checkerUserID, &checkerRef, &checkedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan acceptance criterion with check: %w", err)
		}
		if checkID != nil {
			item.CurrentCheck = &domain.AcceptanceCheck{
				ID:                *checkID,
				CriterionID:       item.Criterion.ID,
				CriterionRevision: *revision,
				Outcome:           *outcome,
				Evidence:          *evidence,
				Checker: domain.Actor{
					Type:   *checkerType,
					UserID: checkerUserID,
					Ref:    derefStr(checkerRef),
				},
				CheckedAt: *checkedAt,
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *AcceptanceStore) Update(
	ctx context.Context,
	criterionID uuid.UUID,
	criterionText, instructions *string,
	position *int,
) (domain.AcceptanceCriterion, error) {
	return s.update(ctx, criterionID, criterionText, instructions, position, nil)
}

func (s *AcceptanceStore) UpdateWithOperation(
	ctx context.Context,
	criterionID uuid.UUID,
	criterionText, instructions *string,
	position *int,
	actor domain.OperationActor,
) (domain.AcceptanceCriterion, error) {
	if err := actor.Validate(); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	return s.update(ctx, criterionID, criterionText, instructions, position, &actor)
}

func (s *AcceptanceStore) update(
	ctx context.Context,
	criterionID uuid.UUID,
	criterionText, instructions *string,
	position *int,
	actor *domain.OperationActor,
) (domain.AcceptanceCriterion, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("begin update acceptance criterion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanCriterion(tx.QueryRow(ctx,
		`SELECT `+criterionColumns+` FROM acceptance_criteria ac WHERE ac.id=$1 FOR UPDATE`, criterionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptanceCriterion{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	old := current
	newText := current.Criterion
	newInstructions := current.VerificationInstructions
	if criterionText != nil {
		newText = *criterionText
	}
	if instructions != nil {
		newInstructions = *instructions
	}
	current.Edit(newText, newInstructions)
	if position != nil {
		current.Move(*position)
	}
	if err := current.Validate(); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE acceptance_criteria
		SET criterion=$2, verification_instructions=$3, revision=$4,
			position=$5, updated_at=now()
		WHERE id=$1`,
		current.ID, current.Criterion, current.VerificationInstructions,
		current.Revision, current.Position)
	if err != nil {
		return domain.AcceptanceCriterion{}, mapPgError(err)
	}
	if actor != nil {
		oldValue, _ := json.Marshal(map[string]any{
			"criterion": old.Criterion, "verification_instructions": old.VerificationInstructions,
			"revision": old.Revision, "position": old.Position,
		})
		newValue, _ := json.Marshal(map[string]any{
			"criterion": current.Criterion, "verification_instructions": current.VerificationInstructions,
			"revision": current.Revision, "position": current.Position,
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: *actor, EntityType: "acceptance_criterion",
			EntityID: current.ID, Action: "updated", OldValue: oldValue, NewValue: newValue,
		}); err != nil {
			return domain.AcceptanceCriterion{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("commit update acceptance criterion: %w", err)
	}
	return s.Get(ctx, criterionID)
}

func (s *AcceptanceStore) AddCheck(ctx context.Context, check domain.AcceptanceCheck) (domain.AcceptanceCheck, error) {
	return s.addCheck(ctx, check, nil)
}

func (s *AcceptanceStore) AddCheckWithOperation(
	ctx context.Context,
	check domain.AcceptanceCheck,
	actor domain.OperationActor,
) (domain.AcceptanceCheck, error) {
	if err := actor.Validate(); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	return s.addCheck(ctx, check, &actor)
}

func (s *AcceptanceStore) addCheck(
	ctx context.Context,
	check domain.AcceptanceCheck,
	actor *domain.OperationActor,
) (domain.AcceptanceCheck, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.AcceptanceCheck{}, fmt.Errorf("begin acceptance check: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	criterion, err := scanCriterion(tx.QueryRow(ctx,
		`SELECT `+criterionColumns+` FROM acceptance_criteria ac WHERE ac.id=$1 FOR UPDATE`,
		check.CriterionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptanceCheck{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if err := check.ValidateAgainst(criterion); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if check.ID == uuid.Nil {
		check.ID = uuid.New()
	}
	if check.CheckedAt.IsZero() {
		check.CheckedAt = time.Now().UTC()
	}
	var userID *uuid.UUID
	var checkerRef *string
	if check.Checker.Type == domain.ActorTypeUser {
		userID = check.Checker.UserID
	} else {
		ref := strings.TrimSpace(check.Checker.Ref)
		checkerRef = &ref
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO acceptance_checks
			(id, criterion_id, criterion_revision, outcome, evidence,
			 checker_type, checked_by_user_id, checker_ref, checked_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING checked_at`,
		check.ID, check.CriterionID, check.CriterionRevision, check.Outcome,
		check.Evidence, check.Checker.Type, userID, checkerRef, check.CheckedAt,
	).Scan(&check.CheckedAt)
	if err != nil {
		return domain.AcceptanceCheck{}, mapPgError(err)
	}
	if actor != nil {
		newValue, _ := json.Marshal(map[string]any{
			"criterion_id": check.CriterionID, "criterion_revision": check.CriterionRevision,
			"outcome": check.Outcome, "evidence": check.Evidence,
			"checker_type": check.Checker.Type, "checked_by_user_id": userID,
			"checker_ref": checkerRef,
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: *actor, EntityType: "acceptance_check",
			EntityID: check.ID, Action: "created", NewValue: newValue,
		}); err != nil {
			return domain.AcceptanceCheck{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptanceCheck{}, fmt.Errorf("commit acceptance check: %w", err)
	}
	return check, nil
}

func (s *AcceptanceStore) RemoveCriterion(
	ctx context.Context,
	criterionID uuid.UUID,
	actor domain.Actor,
	reason string,
) error {
	if !actor.IsHuman() || actor.UserID == nil {
		return fmt.Errorf("%w: only a human user may remove an acceptance criterion", domain.ErrForbidden)
	}
	return s.RemoveCriterionWithOperation(
		ctx, criterionID, domain.SessionOperation(*actor.UserID, "internal"), reason,
	)
}

func (s *AcceptanceStore) RemoveCriterionWithOperation(
	ctx context.Context,
	criterionID uuid.UUID,
	actor domain.OperationActor,
	reason string,
) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin remove acceptance criterion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		projectID       *uuid.UUID
		milestoneID     *uuid.UUID
		milestoneStatus *domain.MilestoneStatus
		taskID          *uuid.UUID
		checkCount      int
	)
	err = tx.QueryRow(ctx, `
		SELECT m.project_id, ac.milestone_id, m.status, ac.task_id,
			(SELECT count(*) FROM acceptance_checks chk WHERE chk.criterion_id=ac.id)
		FROM acceptance_criteria ac
		LEFT JOIN milestones m ON m.id=ac.milestone_id
		WHERE ac.id=$1 AND ac.archived_at IS NULL
		FOR UPDATE OF ac`, criterionID).
		Scan(&projectID, &milestoneID, &milestoneStatus, &taskID, &checkCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock acceptance criterion: %w", err)
	}
	if taskID != nil {
		if checkCount == 0 {
			_, err = tx.Exec(ctx, `DELETE FROM acceptance_criteria WHERE id=$1`, criterionID)
		} else {
			_, err = tx.Exec(ctx,
				`UPDATE acceptance_criteria SET archived_at=now(), updated_at=now() WHERE id=$1`,
				criterionID)
		}
		if err != nil {
			return fmt.Errorf("remove task acceptance criterion: %w", err)
		}
		oldValue, _ := json.Marshal(map[string]any{
			"task_id": taskID, "check_count": checkCount, "reason": strings.TrimSpace(reason),
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "acceptance_criterion",
			EntityID: criterionID, Action: "removed", OldValue: oldValue,
		}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit task criterion removal: %w", err)
		}
		return nil
	}
	if projectID == nil || milestoneID == nil || milestoneStatus == nil {
		return fmt.Errorf("acceptance criterion owner is invalid")
	}
	if *milestoneStatus == domain.MilestoneStatusActive {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%w: a reason is required to change active Milestone scope", domain.ErrInvalidInput)
		}
		var activeMilestoneCriteria int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM acceptance_criteria
			 WHERE milestone_id=$1 AND archived_at IS NULL`,
			*milestoneID,
		).Scan(&activeMilestoneCriteria); err != nil {
			return fmt.Errorf("count active Milestone criteria: %w", err)
		}
		if activeMilestoneCriteria <= 1 {
			return fmt.Errorf("%w: an active Milestone requires an acceptance criterion", domain.ErrConflict)
		}
	}

	action := "acceptance_criterion_archived"
	if *milestoneStatus == domain.MilestoneStatusPlanned && checkCount == 0 {
		action = "acceptance_criterion_removed"
		_, err = tx.Exec(ctx, `DELETE FROM acceptance_criteria WHERE id=$1`, criterionID)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE acceptance_criteria SET archived_at=now(), updated_at=now() WHERE id=$1`,
			criterionID)
	}
	if err != nil {
		return fmt.Errorf("remove acceptance criterion: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity (
			id, project_id, actor_id, action, reason, old_value,
			request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), *projectID, actor.UserID, action, strings.TrimSpace(reason),
		criterionID.String(), actor.RequestID, actor.AuthMethod, actor.TokenID,
		nullIfEmpty(actor.TokenName), actor.AgentRunID)
	if err != nil {
		return fmt.Errorf("record criterion removal: %w", err)
	}
	oldValue, _ := json.Marshal(map[string]any{
		"milestone_id": milestoneID, "check_count": checkCount, "reason": strings.TrimSpace(reason),
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "acceptance_criterion",
		EntityID: criterionID, Action: action, OldValue: oldValue,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit criterion removal: %w", err)
	}
	return nil
}
