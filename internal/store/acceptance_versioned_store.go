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

type CriterionMutation struct {
	Criterion        domain.AcceptanceCriterion
	TaskVersion      *int64
	ProjectVersion   *int64
	MilestoneVersion *int64
}

type AcceptanceCheckMutation struct {
	Check            domain.AcceptanceCheck
	CriterionVersion int64
	TaskVersion      *int64
	ProjectVersion   *int64
	MilestoneVersion *int64
}

func (s *AcceptanceStore) CreateTaskCriterionVersioned(
	ctx context.Context,
	taskID uuid.UUID,
	expectedTaskVersion int64,
	criterion domain.AcceptanceCriterion,
	actor domain.OperationActor,
) (CriterionMutation, error) {
	criterion.TaskID = &taskID
	return s.createVersioned(
		ctx, criterion, actor,
		ownerExpectations{task: &expectedTaskVersion},
	)
}

func (s *AcceptanceStore) CreateMilestoneCriterionVersioned(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	milestoneID uuid.UUID,
	expectedMilestoneVersion int64,
	criterion domain.AcceptanceCriterion,
	actor domain.OperationActor,
) (CriterionMutation, error) {
	criterion.MilestoneID = &milestoneID
	return s.createVersioned(
		ctx, criterion, actor,
		ownerExpectations{
			projectID: projectID, project: &expectedProjectVersion,
			milestone: &expectedMilestoneVersion,
		},
	)
}

type ownerExpectations struct {
	projectID uuid.UUID
	task      *int64
	project   *int64
	milestone *int64
}

func (s *AcceptanceStore) createVersioned(
	ctx context.Context,
	criterion domain.AcceptanceCriterion,
	actor domain.OperationActor,
	expected ownerExpectations,
) (CriterionMutation, error) {
	if err := actor.Validate(); err != nil {
		return CriterionMutation{}, err
	}
	if criterion.ID == uuid.Nil {
		criterion.ID = uuid.New()
	}
	if criterion.Revision == 0 {
		criterion.Revision = 1
	}
	if err := criterion.Validate(); err != nil {
		return CriterionMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return CriterionMutation{}, fmt.Errorf("begin versioned criterion create: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	versions, err := lockCriterionOwnersForCreate(ctx, tx, criterion, expected)
	if err != nil {
		return CriterionMutation{}, err
	}
	created, err := scanCriterion(tx.QueryRow(ctx, `
		INSERT INTO acceptance_criteria
			(id, milestone_id, task_id, criterion,
			 verification_instructions, revision, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+criterionReturnColumns,
		criterion.ID, criterion.MilestoneID, criterion.TaskID,
		criterion.Criterion, criterion.VerificationInstructions,
		criterion.Revision, criterion.Position,
	))
	if err != nil {
		return CriterionMutation{}, mapPgError(err)
	}
	if err := incrementCriterionOwners(ctx, tx, created, &versions); err != nil {
		return CriterionMutation{}, err
	}
	newValue, _ := json.Marshal(map[string]any{
		"milestone_id": created.MilestoneID, "task_id": created.TaskID,
		"criterion":                 created.Criterion,
		"verification_instructions": created.VerificationInstructions,
		"revision":                  created.Revision, "position": created.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor,
		EntityType: "acceptance_criterion", EntityID: created.ID,
		Action: "created", NewValue: newValue,
	}); err != nil {
		return CriterionMutation{}, err
	}
	if err := auditCriterionOwnerChange(ctx, tx, actor, created, "criterion_created"); err != nil {
		return CriterionMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CriterionMutation{}, fmt.Errorf("commit versioned criterion create: %w", err)
	}
	return criterionMutation(created, versions), nil
}

func (s *AcceptanceStore) UpdateCriterionVersioned(
	ctx context.Context,
	criterionID uuid.UUID,
	expectedVersion int64,
	criterionText, instructions *string,
	position *int,
	actor domain.OperationActor,
) (CriterionMutation, error) {
	if err := actor.Validate(); err != nil {
		return CriterionMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return CriterionMutation{}, fmt.Errorf("begin versioned criterion update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, versions, err := lockCriterionAndOwners(ctx, tx, criterionID)
	if err != nil {
		return CriterionMutation{}, err
	}
	if current.Version != expectedVersion {
		return CriterionMutation{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	old := current
	newText, newInstructions := current.Criterion, current.VerificationInstructions
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
		return CriterionMutation{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE acceptance_criteria
		SET criterion=$2, verification_instructions=$3, revision=$4, position=$5,
			version=version+1, updated_at=now()
		WHERE id=$1 AND version=$6
		RETURNING version, updated_at`,
		current.ID, current.Criterion, current.VerificationInstructions,
		current.Revision, current.Position, current.Version,
	).Scan(&current.Version, &current.UpdatedAt)
	if err != nil {
		return CriterionMutation{}, mapPgError(err)
	}
	if err := incrementCriterionOwners(ctx, tx, current, &versions); err != nil {
		return CriterionMutation{}, err
	}
	oldValue, _ := json.Marshal(map[string]any{
		"criterion":                 old.Criterion,
		"verification_instructions": old.VerificationInstructions,
		"revision":                  old.Revision, "position": old.Position,
	})
	newValue, _ := json.Marshal(map[string]any{
		"criterion":                 current.Criterion,
		"verification_instructions": current.VerificationInstructions,
		"revision":                  current.Revision, "position": current.Position,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor,
		EntityType: "acceptance_criterion", EntityID: current.ID,
		Action: "updated", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return CriterionMutation{}, err
	}
	if err := auditCriterionOwnerChange(ctx, tx, actor, current, "criterion_updated"); err != nil {
		return CriterionMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CriterionMutation{}, fmt.Errorf("commit versioned criterion update: %w", err)
	}
	return criterionMutation(current, versions), nil
}

func (s *AcceptanceStore) AddCheckVersioned(
	ctx context.Context,
	criterionID uuid.UUID,
	expectedVersion int64,
	check domain.AcceptanceCheck,
	actor domain.OperationActor,
) (AcceptanceCheckMutation, error) {
	return s.addCheckVersioned(ctx, criterionID, expectedVersion, check, actor, nil)
}

func (s *AcceptanceStore) AddClaimCheckVersioned(
	ctx context.Context,
	claimID uuid.UUID,
	clientKind, clientSessionID string,
	criterionID uuid.UUID,
	expectedVersion int64,
	check domain.AcceptanceCheck,
	actor domain.OperationActor,
) (AcceptanceCheckMutation, error) {
	guard := func(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
		if actor.AuthMethod != domain.AuthenticationMethodAPIToken {
			return nil, fmt.Errorf(
				"%w: Claim checks require a personal API token", domain.ErrForbidden,
			)
		}
		var taskID uuid.UUID
		err := tx.QueryRow(ctx, `
				SELECT c.task_id
				FROM task_claims c
				WHERE c.id=$1
				  AND c.claimed_by_user_id=$2
				  AND c.client_kind=$3
				  AND c.client_session_id=$4
				  AND c.status='active'
				  AND c.expires_at > now()
				FOR SHARE OF c`,
			claimID,
			actor.UserID,
			strings.TrimSpace(clientKind),
			strings.TrimSpace(clientSessionID),
		).Scan(&taskID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf(
				"%w: criterion is not owned by this active Claim", domain.ErrForbidden,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("authorize Claim acceptance check: %w", err)
		}
		return &taskID, nil
	}
	return s.addCheckVersioned(ctx, criterionID, expectedVersion, check, actor, guard)
}

type acceptanceCheckGuard func(context.Context, pgx.Tx) (*uuid.UUID, error)

func (s *AcceptanceStore) addCheckVersioned(
	ctx context.Context,
	criterionID uuid.UUID,
	expectedVersion int64,
	check domain.AcceptanceCheck,
	actor domain.OperationActor,
	guard acceptanceCheckGuard,
) (AcceptanceCheckMutation, error) {
	if err := actor.Validate(); err != nil {
		return AcceptanceCheckMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return AcceptanceCheckMutation{}, fmt.Errorf("begin versioned acceptance check: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var guardedTaskID *uuid.UUID
	if guard != nil {
		guardedTaskID, err = guard(ctx, tx)
		if err != nil {
			return AcceptanceCheckMutation{}, err
		}
	}
	criterion, versions, err := lockCriterionAndOwners(ctx, tx, criterionID)
	if err != nil {
		return AcceptanceCheckMutation{}, err
	}
	if guardedTaskID != nil &&
		(criterion.TaskID == nil || *criterion.TaskID != *guardedTaskID) {
		return AcceptanceCheckMutation{}, fmt.Errorf(
			"%w: criterion is not owned by this active Claim", domain.ErrForbidden,
		)
	}
	if criterion.Version != expectedVersion {
		return AcceptanceCheckMutation{}, domain.VersionConflictError{CurrentVersion: criterion.Version}
	}
	check.CriterionID = criterionID
	if err := check.ValidateAgainst(criterion); err != nil {
		return AcceptanceCheckMutation{}, err
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
		value := strings.TrimSpace(check.Checker.Ref)
		checkerRef = &value
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO acceptance_checks
			(id, criterion_id, criterion_revision, outcome, evidence,
			 checker_type, checked_by_user_id, checker_ref, checked_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING checked_at`,
		check.ID, criterionID, check.CriterionRevision, check.Outcome, check.Evidence,
		check.Checker.Type, userID, checkerRef, check.CheckedAt,
	).Scan(&check.CheckedAt)
	if err != nil {
		return AcceptanceCheckMutation{}, mapPgError(err)
	}
	criterion.Version, err = incrementVersion(
		ctx, tx, "acceptance_criteria", criterion.ID, criterion.Version,
	)
	if err != nil {
		return AcceptanceCheckMutation{}, err
	}
	if err := incrementCriterionOwners(ctx, tx, criterion, &versions); err != nil {
		return AcceptanceCheckMutation{}, err
	}
	newValue, _ := json.Marshal(map[string]any{
		"criterion_id": criterionID, "criterion_revision": check.CriterionRevision,
		"outcome": check.Outcome, "evidence": check.Evidence,
		"checker_type": check.Checker.Type, "checked_by_user_id": userID,
		"checker_ref": checkerRef,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor,
		EntityType: "acceptance_check", EntityID: check.ID,
		Action: "created", NewValue: newValue,
	}); err != nil {
		return AcceptanceCheckMutation{}, err
	}
	if err := auditCriterionOwnerChange(ctx, tx, actor, criterion, "acceptance_checked"); err != nil {
		return AcceptanceCheckMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AcceptanceCheckMutation{}, fmt.Errorf("commit versioned acceptance check: %w", err)
	}
	return AcceptanceCheckMutation{
		Check: check, CriterionVersion: criterion.Version,
		TaskVersion: versions.taskResult(), ProjectVersion: versions.projectResult(),
		MilestoneVersion: versions.milestoneResult(),
	}, nil
}

func (s *AcceptanceStore) RemoveCriterionVersioned(
	ctx context.Context,
	criterionID uuid.UUID,
	expectedVersion int64,
	actor domain.OperationActor,
	reason string,
) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin versioned criterion removal: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	criterion, versions, err := lockCriterionAndOwners(ctx, tx, criterionID)
	if err != nil {
		return err
	}
	if criterion.Version != expectedVersion {
		return domain.VersionConflictError{CurrentVersion: criterion.Version}
	}
	var checkCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM acceptance_checks WHERE criterion_id=$1`,
		criterionID,
	).Scan(&checkCount); err != nil {
		return fmt.Errorf("count criterion checks: %w", err)
	}
	if criterion.MilestoneID != nil {
		var status domain.MilestoneStatus
		if err := tx.QueryRow(ctx,
			`SELECT status FROM milestones WHERE id=$1`,
			*criterion.MilestoneID,
		).Scan(&status); err != nil {
			return fmt.Errorf("read criterion Milestone status: %w", err)
		}
		if status == domain.MilestoneStatusActive {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("%w: a reason is required to change active Milestone scope", domain.ErrInvalidInput)
			}
			var active int
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM acceptance_criteria
				WHERE milestone_id=$1 AND archived_at IS NULL`,
				*criterion.MilestoneID,
			).Scan(&active); err != nil {
				return fmt.Errorf("count active Milestone criteria: %w", err)
			}
			if active <= 1 {
				return fmt.Errorf("%w: an active Milestone requires an acceptance criterion", domain.ErrConflict)
			}
		}
	}
	if checkCount == 0 {
		tag, err := tx.Exec(ctx, `
			DELETE FROM acceptance_criteria WHERE id=$1 AND version=$2`,
			criterionID, criterion.Version,
		)
		if err != nil {
			return fmt.Errorf("delete criterion: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.VersionConflictError{CurrentVersion: criterion.Version}
		}
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE acceptance_criteria
			SET archived_at=now(), version=version+1, updated_at=now()
			WHERE id=$1 AND version=$2`,
			criterionID, criterion.Version,
		)
		if err != nil {
			return fmt.Errorf("archive criterion: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.VersionConflictError{CurrentVersion: criterion.Version}
		}
	}
	if err := incrementCriterionOwners(ctx, tx, criterion, &versions); err != nil {
		return err
	}
	oldValue, _ := json.Marshal(map[string]any{
		"reason": strings.TrimSpace(reason), "check_count": checkCount,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor,
		EntityType: "acceptance_criterion", EntityID: criterionID,
		Action: "removed", OldValue: oldValue,
	}); err != nil {
		return err
	}
	if err := auditCriterionOwnerChange(ctx, tx, actor, criterion, "criterion_removed"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit versioned criterion removal: %w", err)
	}
	return nil
}

type ownerVersions struct {
	projectID    uuid.UUID
	taskID       uuid.UUID
	milestoneID  uuid.UUID
	task         int64
	project      int64
	milestone    int64
	hasTask      bool
	hasProject   bool
	hasMilestone bool
}

func lockCriterionOwnersForCreate(
	ctx context.Context,
	tx pgx.Tx,
	criterion domain.AcceptanceCriterion,
	expected ownerExpectations,
) (ownerVersions, error) {
	var versions ownerVersions
	switch {
	case criterion.TaskID != nil:
		versions.taskID, versions.hasTask = *criterion.TaskID, true
		value, err := lockVersion(ctx, tx, "tasks", versions.taskID)
		if err != nil {
			return ownerVersions{}, err
		}
		if expected.task == nil || value != *expected.task {
			return ownerVersions{}, domain.VersionConflictError{CurrentVersion: value}
		}
		versions.task = value
	case criterion.MilestoneID != nil:
		versions.projectID, versions.hasProject = expected.projectID, true
		versions.milestoneID, versions.hasMilestone = *criterion.MilestoneID, true
		projectVersion, err := lockVersion(ctx, tx, "projects", versions.projectID)
		if err != nil {
			return ownerVersions{}, err
		}
		if expected.project == nil || projectVersion != *expected.project {
			return ownerVersions{}, domain.VersionConflictError{CurrentVersion: projectVersion}
		}
		versions.project = projectVersion
		var actualProjectID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT project_id FROM milestones WHERE id=$1`,
			versions.milestoneID,
		).Scan(&actualProjectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ownerVersions{}, domain.ErrNotFound
			}
			return ownerVersions{}, err
		}
		if actualProjectID != versions.projectID {
			return ownerVersions{}, domain.ErrNotFound
		}
		milestoneVersion, err := lockVersion(ctx, tx, "milestones", versions.milestoneID)
		if err != nil {
			return ownerVersions{}, err
		}
		if expected.milestone == nil || milestoneVersion != *expected.milestone {
			return ownerVersions{}, domain.VersionConflictError{CurrentVersion: milestoneVersion}
		}
		versions.milestone = milestoneVersion
	}
	return versions, nil
}

func lockCriterionAndOwners(
	ctx context.Context,
	tx pgx.Tx,
	criterionID uuid.UUID,
) (domain.AcceptanceCriterion, ownerVersions, error) {
	var milestoneID, taskID *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT milestone_id, task_id
		FROM acceptance_criteria
		WHERE id=$1 AND archived_at IS NULL`,
		criterionID,
	).Scan(&milestoneID, &taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptanceCriterion{}, ownerVersions{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AcceptanceCriterion{}, ownerVersions{}, err
	}
	var versions ownerVersions
	switch {
	case taskID != nil:
		versions.taskID, versions.hasTask = *taskID, true
		versions.task, err = lockVersion(ctx, tx, "tasks", versions.taskID)
	case milestoneID != nil:
		versions.milestoneID, versions.hasMilestone = *milestoneID, true
		err = tx.QueryRow(ctx,
			`SELECT project_id FROM milestones WHERE id=$1`,
			versions.milestoneID,
		).Scan(&versions.projectID)
		if err == nil {
			versions.hasProject = true
			versions.project, err = lockVersion(ctx, tx, "projects", versions.projectID)
		}
		if err == nil {
			versions.milestone, err = lockVersion(ctx, tx, "milestones", versions.milestoneID)
		}
	}
	if err != nil {
		return domain.AcceptanceCriterion{}, ownerVersions{}, err
	}
	criterion, err := scanCriterion(tx.QueryRow(ctx,
		`SELECT `+criterionColumns+`
		 FROM acceptance_criteria ac
		 WHERE ac.id=$1 AND ac.archived_at IS NULL
		 FOR UPDATE`,
		criterionID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptanceCriterion{}, ownerVersions{}, domain.ErrNotFound
	}
	return criterion, versions, err
}

func incrementCriterionOwners(
	ctx context.Context,
	tx pgx.Tx,
	criterion domain.AcceptanceCriterion,
	versions *ownerVersions,
) error {
	var err error
	if versions.hasProject {
		versions.project, err = incrementVersion(
			ctx, tx, "projects", versions.projectID, versions.project,
		)
		if err != nil {
			return err
		}
	}
	if versions.hasMilestone {
		versions.milestone, err = incrementVersion(
			ctx, tx, "milestones", versions.milestoneID, versions.milestone,
		)
		if err != nil {
			return err
		}
	}
	if versions.hasTask {
		versions.task, err = incrementVersion(
			ctx, tx, "tasks", versions.taskID, versions.task,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func criterionMutation(
	criterion domain.AcceptanceCriterion,
	versions ownerVersions,
) CriterionMutation {
	return CriterionMutation{
		Criterion:        criterion,
		TaskVersion:      versions.taskResult(),
		ProjectVersion:   versions.projectResult(),
		MilestoneVersion: versions.milestoneResult(),
	}
}

func (v ownerVersions) taskResult() *int64 {
	if !v.hasTask {
		return nil
	}
	value := v.task
	return &value
}

func (v ownerVersions) projectResult() *int64 {
	if !v.hasProject {
		return nil
	}
	value := v.project
	return &value
}

func (v ownerVersions) milestoneResult() *int64 {
	if !v.hasMilestone {
		return nil
	}
	value := v.milestone
	return &value
}

func auditCriterionOwnerChange(
	ctx context.Context,
	tx pgx.Tx,
	actor domain.OperationActor,
	criterion domain.AcceptanceCriterion,
	action string,
) error {
	value, _ := json.Marshal(map[string]any{
		"criterion_id": criterion.ID, "revision": criterion.Revision,
	})
	if criterion.TaskID != nil {
		var number int64
		if err := tx.QueryRow(ctx,
			`SELECT number FROM tasks WHERE id=$1`,
			*criterion.TaskID,
		).Scan(&number); err != nil {
			return err
		}
		return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "task",
			EntityID: *criterion.TaskID, EntityNumber: &number,
			Action: action, NewValue: value,
		})
	}
	var projectID *uuid.UUID
	if criterion.MilestoneID != nil {
		var resolvedProjectID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT project_id FROM milestones WHERE id=$1`,
			*criterion.MilestoneID,
		).Scan(&resolvedProjectID); err != nil {
			return err
		}
		projectID = &resolvedProjectID
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "milestone",
			EntityID: *criterion.MilestoneID, Action: action, NewValue: value,
		}); err != nil {
			return err
		}
	}
	if projectID != nil {
		var number int64
		if err := tx.QueryRow(ctx,
			`SELECT number FROM projects WHERE id=$1`,
			*projectID,
		).Scan(&number); err != nil {
			return err
		}
		return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "project",
			EntityID: *projectID, EntityNumber: &number,
			Action: action, NewValue: value,
		})
	}
	return nil
}
