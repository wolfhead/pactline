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

type ProjectMembershipStore struct{ db *DB }

func NewProjectMembershipStore(db *DB) *ProjectMembershipStore {
	return &ProjectMembershipStore{db: db}
}

const projectMembershipColumns = `pm.id, pm.project_id, u.id, u.name, u.email,
	pm.role, u.active, pm.created_at, pm.updated_at`

func scanProjectMembership(row scanner) (domain.ProjectMembership, error) {
	var membership domain.ProjectMembership
	if err := row.Scan(
		&membership.ID,
		&membership.ProjectID,
		&membership.User.ID,
		&membership.User.Name,
		&membership.User.Email,
		&membership.Role,
		&membership.Active,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	); err != nil {
		return domain.ProjectMembership{}, fmt.Errorf("scan project membership: %w", err)
	}
	return membership, nil
}

func (s *ProjectMembershipStore) Get(
	ctx context.Context,
	projectID, userID uuid.UUID,
) (domain.ProjectMembership, error) {
	membership, err := scanProjectMembership(s.db.Pool.QueryRow(ctx, `
		SELECT `+projectMembershipColumns+`
		FROM project_memberships pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id=$1 AND pm.user_id=$2`, projectID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectMembership{}, domain.ErrNotFound
	}
	return membership, err
}

func (s *ProjectMembershipStore) List(
	ctx context.Context,
	projectID uuid.UUID,
) ([]domain.ProjectMembership, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+projectMembershipColumns+`
		FROM project_memberships pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id=$1
		ORDER BY CASE pm.role WHEN 'admin' THEN 0 ELSE 1 END, lower(u.name), u.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project memberships: %w", err)
	}
	defer rows.Close()
	memberships := []domain.ProjectMembership{}
	for rows.Next() {
		membership, err := scanProjectMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	return memberships, rows.Err()
}

type ProjectMembershipMutation struct {
	Membership     domain.ProjectMembership
	ProjectVersion int64
}

func (s *ProjectMembershipStore) Add(
	ctx context.Context,
	projectID, userID uuid.UUID,
	role domain.ProjectRole,
	expectedProjectVersion int64,
	actor domain.OperationActor,
) (ProjectMembershipMutation, error) {
	if !role.Valid() {
		return ProjectMembershipMutation{}, fmt.Errorf("%w: invalid project role", domain.ErrInvalidInput)
	}
	return s.mutate(ctx, projectID, userID, role, expectedProjectVersion, actor, "add")
}

func (s *ProjectMembershipStore) ChangeRole(
	ctx context.Context,
	projectID, userID uuid.UUID,
	role domain.ProjectRole,
	expectedProjectVersion int64,
	actor domain.OperationActor,
) (ProjectMembershipMutation, error) {
	if !role.Valid() {
		return ProjectMembershipMutation{}, fmt.Errorf("%w: invalid project role", domain.ErrInvalidInput)
	}
	return s.mutate(ctx, projectID, userID, role, expectedProjectVersion, actor, "change")
}

func (s *ProjectMembershipStore) mutate(
	ctx context.Context,
	projectID, userID uuid.UUID,
	role domain.ProjectRole,
	expectedProjectVersion int64,
	actor domain.OperationActor,
	mode string,
) (ProjectMembershipMutation, error) {
	if err := actor.Validate(); err != nil {
		return ProjectMembershipMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectMembershipMutation{}, fmt.Errorf("begin project membership mutation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockProjectVersion(ctx, tx, projectID, expectedProjectVersion)
	if err != nil {
		return ProjectMembershipMutation{}, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active FROM users WHERE id=$1`, userID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		return ProjectMembershipMutation{}, domain.ErrNotFound
	} else if err != nil {
		return ProjectMembershipMutation{}, fmt.Errorf("load project member user: %w", err)
	}
	if !active {
		return ProjectMembershipMutation{}, fmt.Errorf("%w: inactive users cannot be added to projects", domain.ErrInvalidInput)
	}

	var oldRole domain.ProjectRole
	err = tx.QueryRow(ctx, `
		SELECT role FROM project_memberships
		WHERE project_id=$1 AND user_id=$2 FOR UPDATE`, projectID, userID).Scan(&oldRole)
	if mode == "add" && err == nil {
		return ProjectMembershipMutation{}, fmt.Errorf("%w: user is already a project member", domain.ErrConflict)
	}
	if mode == "change" && errors.Is(err, pgx.ErrNoRows) {
		return ProjectMembershipMutation{}, domain.ErrNotFound
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProjectMembershipMutation{}, fmt.Errorf("lock project membership: %w", err)
	}
	if mode == "change" && oldRole == domain.ProjectRoleAdmin && role != domain.ProjectRoleAdmin {
		if err := requireAnotherActiveProjectAdmin(ctx, tx, projectID, userID); err != nil {
			return ProjectMembershipMutation{}, err
		}
	}

	membershipID := uuid.New()
	if mode == "add" {
		_, err = tx.Exec(ctx, `
			INSERT INTO project_memberships (id, project_id, user_id, role)
			VALUES ($1,$2,$3,$4)`, membershipID, projectID, userID, role)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE project_memberships SET role=$3, updated_at=now()
			WHERE project_id=$1 AND user_id=$2
			RETURNING id`, projectID, userID, role).Scan(&membershipID)
	}
	if err != nil {
		return ProjectMembershipMutation{}, mapPgError(err)
	}
	projectVersion, err = bumpProjectVersion(ctx, tx, projectID, projectVersion)
	if err != nil {
		return ProjectMembershipMutation{}, err
	}
	if err := recordProjectMembershipAudit(
		ctx, tx, projectID, membershipID, userID, oldRole, role, mode, actor,
	); err != nil {
		return ProjectMembershipMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectMembershipMutation{}, fmt.Errorf("commit project membership mutation: %w", err)
	}
	membership, err := s.Get(ctx, projectID, userID)
	return ProjectMembershipMutation{Membership: membership, ProjectVersion: projectVersion}, err
}

func (s *ProjectMembershipStore) Remove(
	ctx context.Context,
	projectID, userID uuid.UUID,
	expectedProjectVersion int64,
	actor domain.OperationActor,
) (int64, error) {
	if err := actor.Validate(); err != nil {
		return 0, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin remove project membership: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockProjectVersion(ctx, tx, projectID, expectedProjectVersion)
	if err != nil {
		return 0, err
	}
	var membershipID uuid.UUID
	var role domain.ProjectRole
	err = tx.QueryRow(ctx, `
		SELECT id, role FROM project_memberships
		WHERE project_id=$1 AND user_id=$2 FOR UPDATE`, projectID, userID).Scan(&membershipID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock project membership: %w", err)
	}
	if role == domain.ProjectRoleAdmin {
		if err := requireAnotherActiveProjectAdmin(ctx, tx, projectID, userID); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_memberships WHERE id=$1`, membershipID); err != nil {
		return 0, fmt.Errorf("remove project membership: %w", err)
	}
	projectVersion, err = bumpProjectVersion(ctx, tx, projectID, projectVersion)
	if err != nil {
		return 0, err
	}
	if err := recordProjectMembershipAudit(
		ctx, tx, projectID, membershipID, userID, role, "", "remove", actor,
	); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit remove project membership: %w", err)
	}
	return projectVersion, nil
}

func lockProjectVersion(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	expected int64,
) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("lock project for membership: %w", err)
	}
	if version != expected {
		return 0, domain.VersionConflictError{CurrentVersion: version}
	}
	return version, nil
}

func requireAnotherActiveProjectAdmin(
	ctx context.Context,
	tx pgx.Tx,
	projectID, excludedUserID uuid.UUID,
) error {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_memberships pm
		JOIN users u ON u.id=pm.user_id AND u.active
		WHERE pm.project_id=$1 AND pm.role='admin' AND pm.user_id<>$2`,
		projectID, excludedUserID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count active project admins: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%w: project must retain an active administrator", domain.ErrConflict)
	}
	return nil
}

func bumpProjectVersion(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, version int64) (int64, error) {
	var next int64
	if err := tx.QueryRow(ctx, `
		UPDATE projects SET version=version+1, updated_at=now()
		WHERE id=$1 AND version=$2 RETURNING version`, projectID, version).Scan(&next); err != nil {
		return 0, fmt.Errorf("increment project version for membership: %w", err)
	}
	return next, nil
}

func recordProjectMembershipAudit(
	ctx context.Context,
	tx pgx.Tx,
	projectID, membershipID, targetUserID uuid.UUID,
	oldRole, newRole domain.ProjectRole,
	action string,
	actor domain.OperationActor,
) error {
	oldValue, _ := json.Marshal(map[string]any{"user_id": targetUserID, "role": oldRole})
	newValue, _ := json.Marshal(map[string]any{"user_id": targetUserID, "role": newRole})
	if action == "add" {
		oldValue = nil
	}
	if action == "remove" {
		newValue = nil
	}
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "project_membership",
		EntityID: membershipID, Action: action, OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, actor_id, action, old_value, new_value,
			 request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), projectID, actor.UserID, "project_membership_"+action,
		strPtrOrNil(string(oldRole)), strPtrOrNil(string(newRole)), actor.RequestID,
		actor.AuthMethod, actor.TokenID, nullIfEmpty(actor.TokenName), actor.AgentRunID,
	)
	if err != nil {
		return fmt.Errorf("record project membership activity: %w", err)
	}
	return nil
}
