package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AgentConversationStore struct{ db *DB }

func NewAgentConversationStore(db *DB) *AgentConversationStore {
	return &AgentConversationStore{db: db}
}

type AgentConversationSnapshot struct {
	Conversation domain.AgentConversation
	Revision     domain.AgentConversationRevision
	Project      *domain.Project
}

type AgentConversationPatch struct {
	Enabled           *bool
	BindingActive     *bool
	DefaultProjectID  *uuid.UUID
	DefaultProjectSet bool
	BusinessContext   *string
}

const agentConversationColumns = `ac.id, ac.provider, ac.tenant_id, ac.external_id,
	ac.name, ac.enabled, ac.binding_active, ac.default_project_id,
	ac.business_context, ac.version, ac.created_by, ac.updated_by,
	ac.last_seen_at, ac.created_at, ac.updated_at`

func (s *AgentConversationStore) Observe(
	ctx context.Context,
	provider, tenantID, externalID, name string,
	actorID uuid.UUID,
	now time.Time,
) (AgentConversationSnapshot, error) {
	provider = strings.TrimSpace(provider)
	tenantID = strings.TrimSpace(tenantID)
	externalID = strings.TrimSpace(externalID)
	name = strings.TrimSpace(name)
	now = now.UTC()
	if provider == "" || tenantID == "" || externalID == "" || actorID == uuid.Nil || now.IsZero() {
		return AgentConversationSnapshot{}, domain.ErrInvalidInput
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("begin observe Agent conversation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row := tx.QueryRow(ctx, `SELECT `+agentConversationColumns+`
		FROM agent_conversations ac
		WHERE ac.provider=$1 AND ac.tenant_id=$2 AND ac.external_id=$3
		FOR UPDATE`, provider, tenantID, externalID)
	conversation, err := scanAgentConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		conversation = domain.AgentConversation{
			ID: uuid.New(), Provider: provider, TenantID: tenantID, ExternalID: externalID,
			Name: name, Enabled: true, Version: 1,
			CreatedBy: actorID, UpdatedBy: actorID,
			LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := conversation.Validate(); err != nil {
			return AgentConversationSnapshot{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_conversations (
			id, provider, tenant_id, external_id, name, enabled, binding_active,
			default_project_id, business_context, version, created_by, updated_by,
			last_seen_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			conversation.ID, conversation.Provider, conversation.TenantID, conversation.ExternalID,
			conversation.Name, conversation.Enabled, conversation.BindingActive,
			conversation.DefaultProjectID, conversation.BusinessContext, conversation.Version,
			conversation.CreatedBy, conversation.UpdatedBy, conversation.LastSeenAt,
			conversation.CreatedAt, conversation.UpdatedAt,
		)
		if err != nil {
			return AgentConversationSnapshot{}, mapPgError(err)
		}
		if err := insertAgentConversationRevision(ctx, tx, conversation); err != nil {
			return AgentConversationSnapshot{}, err
		}
	} else if err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("find Agent conversation: %w", err)
	} else {
		if name == "" {
			name = conversation.Name
		}
		_, err = tx.Exec(ctx, `UPDATE agent_conversations
			SET name=$2, last_seen_at=GREATEST(last_seen_at,$3)
			WHERE id=$1`, conversation.ID, name, now)
		if err != nil {
			return AgentConversationSnapshot{}, fmt.Errorf("observe Agent conversation: %w", err)
		}
		conversation.Name = name
		if now.After(conversation.LastSeenAt) {
			conversation.LastSeenAt = now
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("commit observe Agent conversation: %w", err)
	}
	return s.Get(ctx, conversation.ID)
}

func (s *AgentConversationStore) ObserveRevision(
	ctx context.Context,
	provider, tenantID, externalID string,
	actorID uuid.UUID,
	now time.Time,
) (domain.AgentConversationRevision, error) {
	snapshot, err := s.Observe(ctx, provider, tenantID, externalID, "", actorID, now)
	if err != nil {
		return domain.AgentConversationRevision{}, err
	}
	return snapshot.Revision, nil
}

func (s *AgentConversationStore) ObserveConfiguration(
	ctx context.Context,
	provider, tenantID, externalID string,
	actorID uuid.UUID,
	now time.Time,
) (pactagent.ConversationConfiguration, error) {
	snapshot, err := s.Observe(ctx, provider, tenantID, externalID, "", actorID, now)
	if err != nil {
		return pactagent.ConversationConfiguration{}, err
	}
	return agentConversationConfiguration(snapshot), nil
}

func (s *AgentConversationStore) GetConfigurationRevision(
	ctx context.Context,
	revisionID uuid.UUID,
) (pactagent.ConversationConfiguration, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT
		r.id, r.enabled, r.binding_active, r.default_project_id,
		r.business_context, p.number, p.name, p.archived_at
		FROM agent_conversation_revisions r
		LEFT JOIN projects p ON p.id=r.default_project_id
		WHERE r.id=$1`, revisionID)
	var configuration pactagent.ConversationConfiguration
	var projectName *string
	var archivedAt *time.Time
	err := row.Scan(
		&configuration.RevisionID, &configuration.Enabled,
		&configuration.BindingActive, &configuration.DefaultProjectID,
		&configuration.BusinessContext, &configuration.DefaultProjectNumber,
		&projectName, &archivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.ConversationConfiguration{}, domain.ErrNotFound
	}
	if err != nil {
		return pactagent.ConversationConfiguration{}, fmt.Errorf("get Agent conversation configuration revision: %w", err)
	}
	if projectName != nil {
		configuration.DefaultProjectName = *projectName
	}
	configuration.DefaultProjectArchived = archivedAt != nil
	return configuration, nil
}

func agentConversationConfiguration(snapshot AgentConversationSnapshot) pactagent.ConversationConfiguration {
	configuration := pactagent.ConversationConfiguration{
		RevisionID:       snapshot.Revision.ID,
		Enabled:          snapshot.Revision.Enabled,
		BindingActive:    snapshot.Revision.BindingActive,
		DefaultProjectID: snapshot.Revision.DefaultProjectID,
		BusinessContext:  snapshot.Revision.BusinessContext,
	}
	if snapshot.Project != nil {
		number := snapshot.Project.Number
		configuration.DefaultProjectNumber = &number
		configuration.DefaultProjectName = snapshot.Project.Name
		configuration.DefaultProjectArchived = snapshot.Project.ArchivedAt != nil
	}
	return configuration
}

func (s *AgentConversationStore) Get(
	ctx context.Context,
	id uuid.UUID,
) (AgentConversationSnapshot, error) {
	return scanAgentConversationSnapshot(s.db.Pool.QueryRow(ctx, agentConversationSnapshotQuery+`
		WHERE ac.id=$1`, id))
}

func (s *AgentConversationStore) GetByExternal(
	ctx context.Context,
	provider, tenantID, externalID string,
) (AgentConversationSnapshot, error) {
	return scanAgentConversationSnapshot(s.db.Pool.QueryRow(ctx, agentConversationSnapshotQuery+`
		WHERE ac.provider=$1 AND ac.tenant_id=$2 AND ac.external_id=$3`,
		provider, tenantID, externalID,
	))
}

func (s *AgentConversationStore) GetRevision(
	ctx context.Context,
	id uuid.UUID,
) (domain.AgentConversationRevision, error) {
	revision, err := scanAgentConversationRevision(s.db.Pool.QueryRow(ctx, `
		SELECT id, conversation_id, version, enabled, binding_active,
			default_project_id, business_context, updated_by, created_at
		FROM agent_conversation_revisions WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AgentConversationRevision{}, domain.ErrNotFound
	}
	return revision, err
}

func (s *AgentConversationStore) ListVisible(
	ctx context.Context,
	subject domain.ProjectAccessSubject,
) ([]AgentConversationSnapshot, error) {
	rows, err := s.db.Pool.Query(ctx, agentConversationSnapshotQuery+`
		WHERE $2 OR ac.created_by=$1 OR EXISTS (
			SELECT 1 FROM project_memberships pm
			WHERE pm.project_id=ac.default_project_id AND pm.user_id=$1
		)
		ORDER BY ac.last_seen_at DESC, ac.id`, subject.UserID, subject.IsPlatformAdministrator())
	if err != nil {
		return nil, fmt.Errorf("list visible Agent conversations: %w", err)
	}
	defer rows.Close()
	items := []AgentConversationSnapshot{}
	for rows.Next() {
		item, err := scanAgentConversationSnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *AgentConversationStore) UpdateVersioned(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	patch AgentConversationPatch,
	actorID uuid.UUID,
	now time.Time,
) (AgentConversationSnapshot, error) {
	if id == uuid.Nil || actorID == uuid.Nil || expectedVersion < 1 {
		return AgentConversationSnapshot{}, domain.ErrInvalidInput
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("begin update Agent conversation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	conversation, err := scanAgentConversation(tx.QueryRow(ctx, `SELECT `+agentConversationColumns+`
		FROM agent_conversations ac WHERE ac.id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentConversationSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return AgentConversationSnapshot{}, err
	}
	if conversation.Version != expectedVersion {
		return AgentConversationSnapshot{}, domain.VersionConflictError{CurrentVersion: conversation.Version}
	}
	if patch.Enabled != nil {
		conversation.Enabled = *patch.Enabled
	}
	if patch.BindingActive != nil {
		conversation.BindingActive = *patch.BindingActive
	}
	if patch.DefaultProjectSet {
		conversation.DefaultProjectID = patch.DefaultProjectID
	}
	if patch.BusinessContext != nil {
		conversation.BusinessContext, err = domain.NormalizeAgentConversationContext(*patch.BusinessContext)
		if err != nil {
			return AgentConversationSnapshot{}, err
		}
	}
	conversation.Version++
	conversation.UpdatedBy = actorID
	conversation.UpdatedAt = now.UTC()
	if err := conversation.Validate(); err != nil {
		return AgentConversationSnapshot{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE agent_conversations SET
		enabled=$2, binding_active=$3, default_project_id=$4,
		business_context=$5, version=$6, updated_by=$7, updated_at=$8
		WHERE id=$1`, conversation.ID, conversation.Enabled, conversation.BindingActive,
		conversation.DefaultProjectID, conversation.BusinessContext, conversation.Version,
		conversation.UpdatedBy, conversation.UpdatedAt,
	)
	if err != nil {
		return AgentConversationSnapshot{}, mapPgError(err)
	}
	if err := insertAgentConversationRevision(ctx, tx, conversation); err != nil {
		return AgentConversationSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("commit update Agent conversation: %w", err)
	}
	return s.Get(ctx, id)
}

func insertAgentConversationRevision(
	ctx context.Context,
	tx pgx.Tx,
	conversation domain.AgentConversation,
) error {
	_, err := tx.Exec(ctx, `INSERT INTO agent_conversation_revisions (
		id, conversation_id, version, enabled, binding_active,
		default_project_id, business_context, updated_by, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.New(), conversation.ID,
		conversation.Version, conversation.Enabled, conversation.BindingActive,
		conversation.DefaultProjectID, conversation.BusinessContext,
		conversation.UpdatedBy, conversation.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert Agent conversation revision: %w", err)
	}
	return nil
}

const agentConversationSnapshotQuery = `SELECT ` + agentConversationColumns + `,
	r.id, r.conversation_id, r.version, r.enabled, r.binding_active,
	r.default_project_id, r.business_context, r.updated_by, r.created_at,
	p.id, p.number, p.version, p.name, p.description, p.creator_id,
	p.archived_at, p.created_at, p.updated_at
	FROM agent_conversations ac
	JOIN agent_conversation_revisions r
		ON r.conversation_id=ac.id AND r.version=ac.version
	LEFT JOIN projects p ON p.id=ac.default_project_id `

func scanAgentConversationSnapshot(row scanner) (AgentConversationSnapshot, error) {
	conversation, revision, project, err := scanAgentConversationSnapshotValues(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentConversationSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return AgentConversationSnapshot{}, fmt.Errorf("scan Agent conversation snapshot: %w", err)
	}
	return AgentConversationSnapshot{Conversation: conversation, Revision: revision, Project: project}, nil
}

func scanAgentConversationSnapshotValues(row scanner) (
	domain.AgentConversation,
	domain.AgentConversationRevision,
	*domain.Project,
	error,
) {
	var conversation domain.AgentConversation
	var revision domain.AgentConversationRevision
	var projectID *uuid.UUID
	var projectNumber, projectVersion *int64
	var projectName, projectDescription *string
	var projectCreatorID *uuid.UUID
	var projectArchivedAt, projectCreatedAt, projectUpdatedAt *time.Time
	err := row.Scan(
		&conversation.ID, &conversation.Provider, &conversation.TenantID,
		&conversation.ExternalID, &conversation.Name, &conversation.Enabled,
		&conversation.BindingActive, &conversation.DefaultProjectID,
		&conversation.BusinessContext, &conversation.Version,
		&conversation.CreatedBy, &conversation.UpdatedBy, &conversation.LastSeenAt,
		&conversation.CreatedAt, &conversation.UpdatedAt,
		&revision.ID, &revision.ConversationID, &revision.Version,
		&revision.Enabled, &revision.BindingActive, &revision.DefaultProjectID,
		&revision.BusinessContext, &revision.UpdatedBy, &revision.CreatedAt,
		&projectID, &projectNumber, &projectVersion, &projectName,
		&projectDescription, &projectCreatorID, &projectArchivedAt,
		&projectCreatedAt, &projectUpdatedAt,
	)
	if err != nil {
		return domain.AgentConversation{}, domain.AgentConversationRevision{}, nil, err
	}
	var project *domain.Project
	if projectID != nil {
		project = &domain.Project{
			ID: *projectID, Number: *projectNumber, Version: *projectVersion,
			Name: *projectName, Description: *projectDescription,
			CreatorID: *projectCreatorID, ArchivedAt: projectArchivedAt,
			CreatedAt: *projectCreatedAt, UpdatedAt: *projectUpdatedAt,
		}
	}
	return conversation, revision, project, nil
}

func scanAgentConversation(row scanner) (domain.AgentConversation, error) {
	var conversation domain.AgentConversation
	err := row.Scan(
		&conversation.ID, &conversation.Provider, &conversation.TenantID,
		&conversation.ExternalID, &conversation.Name, &conversation.Enabled,
		&conversation.BindingActive, &conversation.DefaultProjectID,
		&conversation.BusinessContext, &conversation.Version,
		&conversation.CreatedBy, &conversation.UpdatedBy, &conversation.LastSeenAt,
		&conversation.CreatedAt, &conversation.UpdatedAt,
	)
	return conversation, err
}

func scanAgentConversationRevision(row scanner) (domain.AgentConversationRevision, error) {
	var revision domain.AgentConversationRevision
	err := row.Scan(
		&revision.ID, &revision.ConversationID, &revision.Version,
		&revision.Enabled, &revision.BindingActive, &revision.DefaultProjectID,
		&revision.BusinessContext, &revision.UpdatedBy, &revision.CreatedAt,
	)
	if err != nil {
		return domain.AgentConversationRevision{}, err
	}
	return revision, nil
}
