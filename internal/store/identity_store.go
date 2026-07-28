package store

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/identity"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type IdentityStore struct {
	db *DB
}

var primarySeedUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func NewIdentityStore(db *DB) *IdentityStore {
	return &IdentityStore{db: db}
}

func (s *IdentityStore) CreateAuthorizationTransaction(ctx context.Context, transaction identity.AuthorizationTransaction) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO authorization_transactions
			(id, purpose, state_hash, invitation_id, expires_at, consumed_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		transaction.ID, transaction.Purpose, transaction.StateHash, transaction.InvitationID,
		transaction.ExpiresAt, transaction.ConsumedAt, transaction.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return identity.ErrAuthorizationInvalid
		}
		return fmt.Errorf("create authorization transaction: %w", err)
	}
	return nil
}

func (s *IdentityStore) ConsumeAuthorizationState(ctx context.Context, stateHash []byte, now time.Time) (identity.AuthorizationTransaction, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return identity.AuthorizationTransaction{}, fmt.Errorf("begin authorization state consumption: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	transaction, err := scanAuthorization(tx.QueryRow(ctx, `
		SELECT id, purpose, state_hash, invitation_id, expires_at, consumed_at, created_at
		FROM authorization_transactions
		WHERE state_hash=$1
		FOR UPDATE`, stateHash))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (transaction.ConsumedAt != nil || !now.Before(transaction.ExpiresAt)) {
		return identity.AuthorizationTransaction{}, identity.ErrAuthorizationInvalid
	}
	if err != nil {
		return identity.AuthorizationTransaction{}, fmt.Errorf("load authorization transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE authorization_transactions SET consumed_at=$2 WHERE id=$1`, transaction.ID, now); err != nil {
		return identity.AuthorizationTransaction{}, fmt.Errorf("consume authorization transaction: %w", err)
	}
	transaction.ConsumedAt = &now
	if err := tx.Commit(ctx); err != nil {
		return identity.AuthorizationTransaction{}, fmt.Errorf("commit authorization state consumption: %w", err)
	}
	return transaction, nil
}

func (s *IdentityStore) CreateInvitation(ctx context.Context, invitation identity.Invitation, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "create invitation", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO invitations
				(id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
				 created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			invitation.ID, invitation.Target.Provider, invitation.Target.TenantID, invitation.Target.SubjectID,
			jsonOrEmpty(invitation.TargetSnapshot), invitation.TokenHash, invitation.Status,
			invitation.CreatedByUserID, invitation.ExpiresAt, invitation.AcceptedByUserID,
			invitation.AcceptedAt, invitation.RevokedAt, invitation.CreatedAt, invitation.UpdatedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return identity.ErrInvitationConflict
			}
			return fmt.Errorf("insert invitation: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) ListInvitations(ctx context.Context) ([]identity.Invitation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
		       created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at
		FROM invitations ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()
	var out []identity.Invitation
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		out = append(out, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invitations: %w", err)
	}
	return out, nil
}

func (s *IdentityStore) GetInvitation(ctx context.Context, id uuid.UUID) (identity.Invitation, error) {
	invitation, err := scanInvitation(s.db.Pool.QueryRow(ctx, `
		SELECT id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
		       created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at
		FROM invitations WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invitation{}, identity.ErrInvitationInvalid
	}
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("get invitation: %w", err)
	}
	return invitation, nil
}

func (s *IdentityStore) GetInvitationByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (identity.Invitation, error) {
	invitation, err := scanInvitation(s.db.Pool.QueryRow(ctx, `
		SELECT id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
		       created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at
		FROM invitations WHERE token_hash=$1`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) || err == nil &&
		(invitation.Status != identity.InvitationPending || !now.Before(invitation.ExpiresAt)) {
		return identity.Invitation{}, identity.ErrInvitationInvalid
	}
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("get invitation by token: %w", err)
	}
	return invitation, nil
}

func (s *IdentityStore) RevokeInvitation(ctx context.Context, id uuid.UUID, now time.Time, audit identity.AuditEvent) error {
	return s.changeInvitation(ctx, "revoke invitation", id, audit, func(tx pgx.Tx, invitation identity.Invitation) error {
		if invitation.Status != identity.InvitationPending || !now.Before(invitation.ExpiresAt) {
			return identity.ErrInvitationInvalid
		}
		tag, err := tx.Exec(ctx, `
			UPDATE invitations SET status='revoked', revoked_at=$2, updated_at=$2
			WHERE id=$1 AND status='pending'`, id, now)
		if err != nil {
			return fmt.Errorf("update invitation: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return identity.ErrInvitationInvalid
		}
		return nil
	})
}

func (s *IdentityStore) RotateInvitation(ctx context.Context, id, actorID uuid.UUID, tokenHash []byte, expiresAt time.Time) (identity.Invitation, error) {
	now := time.Now().UTC()
	audit := identity.AuditEvent{
		ID: uuid.New(), EventType: "invitation_rotated", ActorUserID: &actorID,
		InvitationID: &id, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	var updated identity.Invitation
	err := s.changeInvitation(ctx, "rotate invitation", id, audit, func(tx pgx.Tx, invitation identity.Invitation) error {
		if invitation.Status != identity.InvitationPending || !now.Before(invitation.ExpiresAt) {
			return identity.ErrInvitationInvalid
		}
		if _, err := tx.Exec(ctx, `
			UPDATE invitations SET token_hash=$2, expires_at=$3, updated_at=$4
			WHERE id=$1`, id, tokenHash, expiresAt, now); err != nil {
			if isUniqueViolation(err) {
				return identity.ErrInvitationInvalid
			}
			return fmt.Errorf("rotate invitation token: %w", err)
		}
		invitation.TokenHash = append([]byte(nil), tokenHash...)
		invitation.ExpiresAt = expiresAt
		invitation.UpdatedAt = now
		updated = invitation
		return nil
	})
	return updated, err
}

func (s *IdentityStore) ExpireInvitations(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE invitations
		SET status='expired', updated_at=$1
		WHERE status='pending' AND expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("expire invitations: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *IdentityStore) AcceptInvitation(ctx context.Context, command identity.AcceptInvitationCommand) (domain.User, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	lookup := `token_hash=$1`
	lookupValue := any(command.TokenHash)
	if command.InvitationID != uuid.Nil {
		lookup = `id=$1`
		lookupValue = command.InvitationID
	}
	invitation, err := scanInvitation(tx.QueryRow(ctx, `
		SELECT id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
		       created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at
		FROM invitations WHERE `+lookup+` FOR UPDATE`, lookupValue))
	if errors.Is(err, pgx.ErrNoRows) ||
		err == nil && !identity.InvitationMatches(invitation, command.Principal.Key, command.Now) {
		return domain.User{}, identity.ErrInvitationInvalid
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("load invitation for acceptance: %w", err)
	}

	user := domain.User{
		ID: command.UserID, Name: command.UserName, Email: command.UserEmail,
		AvatarURL: command.UserAvatarURL, PlatformRole: domain.PlatformRoleMember,
		Active: true, CreatedAt: command.Now, UpdatedAt: command.Now,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, name, email, avatar_url, platform_role, roles, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'MEMBER','{}',true,$5,$5)`,
		user.ID, user.Name, user.Email, user.AvatarURL, command.Now)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, identity.ErrInvitationInvalid
		}
		return domain.User{}, fmt.Errorf("create invited user: %w", err)
	}
	externalID := uuid.New()
	if err := insertExternalIdentityAndCredential(ctx, tx, externalID, user.ID, command.Principal, command.Credential, command.Now); err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, identity.ErrInvitationInvalid
		}
		return domain.User{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE invitations
		SET status='accepted', accepted_by_user_id=$2, accepted_at=$3, updated_at=$3
		WHERE id=$1 AND status='pending'`, invitation.ID, user.ID, command.Now)
	if err != nil {
		return domain.User{}, fmt.Errorf("consume invitation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.User{}, identity.ErrInvitationInvalid
	}
	if command.Session.ID != uuid.Nil {
		if err := insertSession(ctx, tx, command.Session); err != nil {
			return domain.User{}, err
		}
	}
	command.Audit.InvitationID = &invitation.ID
	command.Audit.SubjectUserID = &user.ID
	if err := insertAudit(ctx, tx, command.Audit); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, identity.ErrInvitationInvalid
		}
		return domain.User{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return user, nil
}

func (s *IdentityStore) BindExternalIdentity(ctx context.Context, external identity.ExternalIdentity, credential identity.OAuthCredential, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "bind external identity", func(tx pgx.Tx) error {
		principal := identity.Principal{Key: external.Key, Profile: external.ProviderProfile}
		if err := insertExternalIdentityAndCredential(ctx, tx, external.ID, external.UserID, principal, credential, external.CreatedAt); err != nil {
			if isUniqueViolation(err) {
				return domain.ErrConflict
			}
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) GetCredential(ctx context.Context, externalIdentityID uuid.UUID) (identity.OAuthCredential, error) {
	return scanCredential(s.db.Pool.QueryRow(ctx, `
		SELECT access_token_ciphertext, refresh_token_ciphertext,
		       access_token_expires_at, refresh_token_expires_at, encryption_key_id
		FROM oauth_credentials WHERE external_identity_id=$1`, externalIdentityID))
}

func (s *IdentityStore) GetExternalIdentityForUser(ctx context.Context, userID uuid.UUID) (identity.ExternalIdentity, error) {
	var external identity.ExternalIdentity
	err := s.db.Pool.QueryRow(ctx, `
		SELECT e.id, e.user_id, e.provider, e.tenant_id, e.subject_id, e.provider_profile,
		       e.last_verified_at, e.created_at, e.updated_at,
		       c.access_token_ciphertext, c.refresh_token_ciphertext,
		       c.access_token_expires_at, c.refresh_token_expires_at, c.encryption_key_id
		FROM external_identities e
		JOIN oauth_credentials c ON c.external_identity_id=e.id
		WHERE e.user_id=$1`, userID).Scan(
		&external.ID, &external.UserID, &external.Key.Provider, &external.Key.TenantID,
		&external.Key.SubjectID, &external.ProviderProfile, &external.LastVerifiedAt,
		&external.CreatedAt, &external.UpdatedAt,
		&external.EncryptedCredential.AccessTokenCiphertext,
		&external.EncryptedCredential.RefreshTokenCiphertext,
		&external.EncryptedCredential.AccessTokenExpiresAt,
		&external.EncryptedCredential.RefreshTokenExpiresAt,
		&external.EncryptedCredential.EncryptionKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ExternalIdentity{}, identity.ErrCredentialNotFound
	}
	if err != nil {
		return identity.ExternalIdentity{}, fmt.Errorf("get user external identity: %w", err)
	}
	return external, nil
}

func (s *IdentityStore) FindExternalIdentity(ctx context.Context, key identity.PrincipalKey) (identity.ExternalIdentity, domain.User, error) {
	var external identity.ExternalIdentity
	var user domain.User
	var roles []string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT e.id, e.user_id, e.provider, e.tenant_id, e.subject_id, e.provider_profile,
		       e.last_verified_at, e.created_at, e.updated_at,
		       c.access_token_ciphertext, c.refresh_token_ciphertext,
		       c.access_token_expires_at, c.refresh_token_expires_at, c.encryption_key_id,
		       u.id, u.name, u.email, u.avatar_url, u.platform_role, u.roles, u.active, u.created_at, u.updated_at
		FROM external_identities e
		JOIN oauth_credentials c ON c.external_identity_id=e.id
		JOIN users u ON u.id=e.user_id
		WHERE e.provider=$1 AND e.tenant_id=$2 AND e.subject_id=$3`,
		key.Provider, key.TenantID, key.SubjectID).Scan(
		&external.ID, &external.UserID, &external.Key.Provider, &external.Key.TenantID,
		&external.Key.SubjectID, &external.ProviderProfile, &external.LastVerifiedAt,
		&external.CreatedAt, &external.UpdatedAt,
		&external.EncryptedCredential.AccessTokenCiphertext,
		&external.EncryptedCredential.RefreshTokenCiphertext,
		&external.EncryptedCredential.AccessTokenExpiresAt,
		&external.EncryptedCredential.RefreshTokenExpiresAt,
		&external.EncryptedCredential.EncryptionKeyID,
		&user.ID, &user.Name, &user.Email, &user.AvatarURL, &user.PlatformRole,
		&roles, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ExternalIdentity{}, domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return identity.ExternalIdentity{}, domain.User{}, fmt.Errorf("find external identity: %w", err)
	}
	user.Roles = userRoles(roles)
	return external, user, nil
}

func (s *IdentityStore) BootstrapAdmin(ctx context.Context, command identity.BootstrapAdminCommand) (domain.User, error) {
	var user domain.User
	err := s.inTransaction(ctx, "bootstrap administrator", func(tx pgx.Tx) error {
		var adminCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE platform_role='ADMIN'`).Scan(&adminCount); err != nil {
			return fmt.Errorf("count administrators: %w", err)
		}
		if adminCount != 0 {
			return identity.ErrLoginDenied
		}
		var roles []string
		err := tx.QueryRow(ctx, `
			SELECT id,name,email,avatar_url,platform_role,roles,active,created_at,updated_at
			FROM users WHERE id=$1 FOR UPDATE`, primarySeedUserID).Scan(
			&user.ID, &user.Name, &user.Email, &user.AvatarURL, &user.PlatformRole,
			&roles, &user.Active, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return fmt.Errorf("lock bootstrap user: %w", err)
		}
		user.Roles = userRoles(roles)
		_, err = tx.Exec(ctx, `
			UPDATE users SET name=$2,email=$3,avatar_url=$4,platform_role='ADMIN',active=true,updated_at=$5
			WHERE id=$1`, primarySeedUserID, command.Principal.Name, command.Principal.Email,
			command.Principal.AvatarURL, command.Now)
		if err != nil {
			if isUniqueViolation(err) {
				return identity.ErrLoginDenied
			}
			return fmt.Errorf("update bootstrap user: %w", err)
		}
		externalID := uuid.New()
		if err := insertExternalIdentityAndCredential(ctx, tx, externalID, primarySeedUserID,
			command.Principal, command.Credential, command.Now); err != nil {
			if isUniqueViolation(err) {
				return identity.ErrLoginDenied
			}
			return err
		}
		if err := insertSession(ctx, tx, command.Session); err != nil {
			return err
		}
		command.Audit.SubjectUserID = &primarySeedUserID
		command.Audit.SessionID = &command.Session.ID
		if err := insertAudit(ctx, tx, command.Audit); err != nil {
			return err
		}
		user.Name, user.Email, user.AvatarURL = command.Principal.Name, command.Principal.Email, command.Principal.AvatarURL
		user.PlatformRole, user.Active, user.UpdatedAt = domain.PlatformRoleAdmin, true, command.Now
		return nil
	})
	return user, err
}

func (s *IdentityStore) LoginExternal(ctx context.Context, command identity.LoginCommand) (domain.User, error) {
	var user domain.User
	err := s.inTransaction(ctx, "login external identity", func(tx pgx.Tx) error {
		var externalID uuid.UUID
		var roles []string
		err := tx.QueryRow(ctx, `
			SELECT e.id,u.id,u.name,u.email,u.avatar_url,u.platform_role,u.roles,u.active,u.created_at,u.updated_at
			FROM external_identities e JOIN users u ON u.id=e.user_id
			WHERE e.provider=$1 AND e.tenant_id=$2 AND e.subject_id=$3
			FOR UPDATE OF e,u`,
			command.Principal.Key.Provider, command.Principal.Key.TenantID, command.Principal.Key.SubjectID).Scan(
			&externalID, &user.ID, &user.Name, &user.Email, &user.AvatarURL,
			&user.PlatformRole, &roles, &user.Active, &user.CreatedAt, &user.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !user.Active {
			return identity.ErrLoginDenied
		}
		if err != nil {
			return fmt.Errorf("lock external login identity: %w", err)
		}
		user.Roles = userRoles(roles)
		_, err = tx.Exec(ctx, `
			UPDATE external_identities SET provider_profile=$2,last_verified_at=$3,updated_at=$3 WHERE id=$1`,
			externalID, jsonOrEmpty(command.Principal.Profile), command.Now)
		if err != nil {
			return fmt.Errorf("update external profile: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE oauth_credentials SET access_token_ciphertext=$2,refresh_token_ciphertext=$3,
			    access_token_expires_at=$4,refresh_token_expires_at=$5,encryption_key_id=$6,updated_at=$7
			WHERE external_identity_id=$1`,
			externalID, command.Credential.AccessTokenCiphertext, command.Credential.RefreshTokenCiphertext,
			command.Credential.AccessTokenExpiresAt, command.Credential.RefreshTokenExpiresAt,
			command.Credential.EncryptionKeyID, command.Now)
		if err != nil {
			return fmt.Errorf("update login credential: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE users SET name=$2,email=$3,avatar_url=$4,updated_at=$5 WHERE id=$1`,
			user.ID, command.Principal.Name, command.Principal.Email, command.Principal.AvatarURL, command.Now)
		if err != nil {
			return fmt.Errorf("update login user profile: %w", err)
		}
		if err := insertSession(ctx, tx, command.Session); err != nil {
			return err
		}
		command.Audit.SubjectUserID = &user.ID
		command.Audit.SessionID = &command.Session.ID
		if err := insertAudit(ctx, tx, command.Audit); err != nil {
			return err
		}
		user.Name, user.Email, user.AvatarURL, user.UpdatedAt =
			command.Principal.Name, command.Principal.Email, command.Principal.AvatarURL, command.Now
		return nil
	})
	return user, err
}

func (s *IdentityStore) RefreshCredentialLocked(
	ctx context.Context,
	externalIdentityID uuid.UUID,
	refresh func(identity.OAuthCredential) (identity.OAuthCredential, error),
) (identity.OAuthCredential, error) {
	var updated identity.OAuthCredential
	err := s.inTransaction(ctx, "refresh credential", func(tx pgx.Tx) error {
		current, err := scanCredential(tx.QueryRow(ctx, `
			SELECT access_token_ciphertext,refresh_token_ciphertext,access_token_expires_at,
			       refresh_token_expires_at,encryption_key_id
			FROM oauth_credentials WHERE external_identity_id=$1 FOR UPDATE`, externalIdentityID))
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrCredentialNotFound
		}
		if err != nil {
			return fmt.Errorf("lock refresh credential: %w", err)
		}
		updated, err = refresh(current)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE oauth_credentials SET access_token_ciphertext=$2,refresh_token_ciphertext=$3,
			    access_token_expires_at=$4,refresh_token_expires_at=$5,encryption_key_id=$6,updated_at=now()
			WHERE external_identity_id=$1`,
			externalIdentityID, updated.AccessTokenCiphertext, updated.RefreshTokenCiphertext,
			updated.AccessTokenExpiresAt, updated.RefreshTokenExpiresAt, updated.EncryptionKeyID)
		return err
	})
	return updated, err
}

func (s *IdentityStore) UpdateCredential(ctx context.Context, externalIdentityID uuid.UUID, credential identity.OAuthCredential) error {
	return s.inTransaction(ctx, "update credential", func(tx pgx.Tx) error {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT external_identity_id FROM oauth_credentials
			WHERE external_identity_id=$1 FOR UPDATE`, externalIdentityID).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrCredentialNotFound
		} else if err != nil {
			return fmt.Errorf("lock credential: %w", err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE oauth_credentials
			SET access_token_ciphertext=$2, refresh_token_ciphertext=$3,
			    access_token_expires_at=$4, refresh_token_expires_at=$5,
			    encryption_key_id=$6, updated_at=now()
			WHERE external_identity_id=$1`,
			externalIdentityID, credential.AccessTokenCiphertext, credential.RefreshTokenCiphertext,
			credential.AccessTokenExpiresAt, credential.RefreshTokenExpiresAt, credential.EncryptionKeyID)
		if err != nil {
			return fmt.Errorf("update credential: %w", err)
		}
		return nil
	})
}

func (s *IdentityStore) CreateSession(ctx context.Context, session identity.Session, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "create session", func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `
			SELECT active FROM users WHERE id=$1 FOR UPDATE`, session.UserID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrSessionInvalid
		} else if err != nil {
			return fmt.Errorf("lock session user: %w", err)
		}
		if !active {
			return identity.ErrUserInactive
		}
		if err := insertSession(ctx, tx, session); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

// LogoutSession atomically closes any active impersonation, revokes the
// session, and appends the corresponding audit events. The session owner is
// loaded under lock and remains both actor and subject of session revocation;
// an impersonated Member is only the subject of the impersonation-ended event.
func (s *IdentityStore) LogoutSession(ctx context.Context, sessionID uuid.UUID, now time.Time, requestID string) error {
	return s.inTransaction(ctx, "logout session", func(tx pgx.Tx) error {
		var ownerID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT user_id FROM sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&ownerID); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrSessionInvalid
		} else if err != nil {
			return fmt.Errorf("lock logout session: %w", err)
		}

		var impersonationID, subjectID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id, subject_user_id
			FROM impersonations
			WHERE session_id=$1 AND ended_at IS NULL
			FOR UPDATE`, sessionID).Scan(&impersonationID, &subjectID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Ending impersonation is idempotent at logout. A concurrent
			// boundary change may have already closed it while waiting for
			// the session lock.
		case err != nil:
			return fmt.Errorf("lock logout impersonation: %w", err)
		default:
			if _, err := tx.Exec(ctx, `UPDATE impersonations SET ended_at=$2 WHERE id=$1`, impersonationID, now); err != nil {
				return fmt.Errorf("end impersonation during logout: %w", err)
			}
			audit := identity.AuditEvent{
				ID: uuid.New(), EventType: "impersonation_ended", ActorUserID: &ownerID,
				SubjectUserID: &subjectID, SessionID: &sessionID,
				Metadata: json.RawMessage(`{"reason":"logout"}`), OccurredAt: now,
			}
			if requestID != "" {
				audit.RequestID = &requestID
			}
			if err := insertAudit(ctx, tx, audit); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE sessions
			SET revoked_at=COALESCE(revoked_at,$2), revoke_reason=COALESCE(revoke_reason,'logout')
			WHERE id=$1`, sessionID, now); err != nil {
			return fmt.Errorf("revoke session during logout: %w", err)
		}
		audit := identity.AuditEvent{
			ID: uuid.New(), EventType: "session_revoked", ActorUserID: &ownerID,
			SubjectUserID: &ownerID, SessionID: &sessionID,
			Metadata: json.RawMessage(`{"reason":"logout"}`), OccurredAt: now,
		}
		if requestID != "" {
			audit.RequestID = &requestID
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) ResolveSession(ctx context.Context, id uuid.UUID, secretHash []byte, now time.Time) (identity.SessionBundle, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.secret_hash, s.csrf_secret_hash, s.created_at, s.last_seen_at,
		       s.idle_expires_at, s.absolute_expires_at, s.last_provider_verified_at,
		       s.provider_failure_since, s.revoked_at, s.revoke_reason,
		       u.id, u.name, u.email, u.avatar_url, u.platform_role, u.roles, u.active, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.id=$1`, id)
	var bundle identity.SessionBundle
	var roles []string
	if err := row.Scan(
		&bundle.Session.ID, &bundle.Session.UserID, &bundle.Session.SecretHash, &bundle.Session.CSRFSecretHash,
		&bundle.Session.CreatedAt, &bundle.Session.LastSeenAt, &bundle.Session.IdleExpiresAt,
		&bundle.Session.AbsoluteExpiresAt, &bundle.Session.LastProviderVerifiedAt,
		&bundle.Session.ProviderFailureSince, &bundle.Session.RevokedAt, &bundle.Session.RevokeReason,
		&bundle.User.ID, &bundle.User.Name, &bundle.User.Email, &bundle.User.AvatarURL,
		&bundle.User.PlatformRole, &roles, &bundle.User.Active, &bundle.User.CreatedAt, &bundle.User.UpdatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return identity.SessionBundle{}, identity.ErrSessionInvalid
	} else if err != nil {
		return identity.SessionBundle{}, fmt.Errorf("resolve session: %w", err)
	}
	bundle.User.Roles = userRoles(roles)
	if len(secretHash) != len(bundle.Session.SecretHash) ||
		subtle.ConstantTimeCompare(secretHash, bundle.Session.SecretHash) != 1 {
		return identity.SessionBundle{}, identity.ErrSessionInvalid
	}
	if !bundle.User.Active {
		return identity.SessionBundle{}, identity.ErrUserInactive
	}
	if bundle.Session.RevokedAt != nil {
		return identity.SessionBundle{}, identity.ErrSessionRevoked
	}
	if !now.Before(bundle.Session.IdleExpiresAt) || !now.Before(bundle.Session.AbsoluteExpiresAt) {
		return identity.SessionBundle{}, identity.ErrSessionExpired
	}
	impersonation, subject, err := s.currentImpersonation(ctx, bundle.Session.ID)
	if err != nil {
		return identity.SessionBundle{}, err
	}
	bundle.Impersonation, bundle.Subject = impersonation, subject
	return bundle, nil
}

func (s *IdentityStore) TouchSession(ctx context.Context, id uuid.UUID, now, idleExpiresAt time.Time) (bool, error) {
	var changed bool
	err := s.inTransaction(ctx, "touch session", func(tx pgx.Tx) error {
		session, err := scanSession(tx.QueryRow(ctx, `
			SELECT id, user_id, secret_hash, csrf_secret_hash, created_at, last_seen_at,
			       idle_expires_at, absolute_expires_at, last_provider_verified_at,
			       provider_failure_since, revoked_at, revoke_reason
			FROM sessions WHERE id=$1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrSessionInvalid
		}
		if err != nil {
			return fmt.Errorf("lock session: %w", err)
		}
		if !identity.SessionRollDue(session, now) {
			return nil
		}
		if idleExpiresAt.After(session.AbsoluteExpiresAt) {
			idleExpiresAt = session.AbsoluteExpiresAt
		}
		_, err = tx.Exec(ctx, `UPDATE sessions SET last_seen_at=$2, idle_expires_at=$3 WHERE id=$1`, id, now, idleExpiresAt)
		if err != nil {
			return fmt.Errorf("roll session: %w", err)
		}
		changed = true
		return nil
	})
	return changed, err
}

func (s *IdentityStore) RecordProviderVerification(ctx context.Context, sessionID uuid.UUID, verifiedAt time.Time) error {
	return s.lockSessionUpdate(ctx, sessionID, `
		UPDATE sessions SET last_provider_verified_at=$2, provider_failure_since=NULL WHERE id=$1`,
		verifiedAt)
}

func (s *IdentityStore) RecordProviderFailure(ctx context.Context, sessionID uuid.UUID, failedAt time.Time, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "record provider failure", func(tx pgx.Tx) error {
		if err := lockSession(ctx, tx, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET provider_failure_since=COALESCE(provider_failure_since,$2)
			WHERE id=$1`, sessionID, failedAt); err != nil {
			return fmt.Errorf("record provider failure: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) RevokeSession(ctx context.Context, id uuid.UUID, reason string, now time.Time, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "revoke session", func(tx pgx.Tx) error {
		if err := lockSession(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2), revoke_reason=COALESCE(revoke_reason,$3)
			WHERE id=$1`, id, now, reason); err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) RevokeAllSessions(ctx context.Context, userID uuid.UUID, reason string, now time.Time, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "revoke user sessions", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2), revoke_reason=COALESCE(revoke_reason,$3)
			WHERE user_id=$1 AND revoked_at IS NULL`, userID, now, reason); err != nil {
			return fmt.Errorf("revoke user sessions: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) DeactivateUser(ctx context.Context, userID, actorID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	return s.inTransaction(ctx, "deactivate user", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET active=false, updated_at=$2 WHERE id=$1`, userID, now)
		if err != nil {
			return fmt.Errorf("deactivate user: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2), revoke_reason=COALESCE(revoke_reason,$3)
			WHERE user_id=$1 AND revoked_at IS NULL`, userID, now, reason); err != nil {
			return fmt.Errorf("revoke deactivated user sessions: %w", err)
		}
		return insertAudit(ctx, tx, identity.AuditEvent{
			ID: uuid.New(), EventType: "user_deactivated", ActorUserID: &actorID,
			SubjectUserID: &userID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
		})
	})
}

func (s *IdentityStore) ReactivateUser(ctx context.Context, userID, actorID uuid.UUID) error {
	now := time.Now().UTC()
	return s.inTransaction(ctx, "reactivate user", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET active=true, updated_at=$2 WHERE id=$1`, userID, now)
		if err != nil {
			return fmt.Errorf("reactivate user: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, identity.AuditEvent{
			ID: uuid.New(), EventType: "user_reactivated", ActorUserID: &actorID,
			SubjectUserID: &userID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
		})
	})
}

func (s *IdentityStore) StartImpersonation(ctx context.Context, impersonation identity.Impersonation, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "start impersonation", func(tx pgx.Tx) error {
		var sessionUserID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT user_id FROM sessions WHERE id=$1 FOR UPDATE`,
			impersonation.SessionID).Scan(&sessionUserID); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrSessionInvalid
		} else if err != nil {
			return fmt.Errorf("lock impersonation session: %w", err)
		}
		if sessionUserID != impersonation.ActorUserID {
			return identity.ErrImpersonationDenied
		}
		if impersonation.ActorUserID == impersonation.SubjectUserID {
			return identity.ErrImpersonationDenied
		}
		var actorRole, subjectRole domain.PlatformRole
		var actorActive, subjectActive bool
		if err := tx.QueryRow(ctx, `
			SELECT a.platform_role, a.active, sub.platform_role, sub.active
			FROM users a CROSS JOIN users sub
			WHERE a.id=$1 AND sub.id=$2
			FOR UPDATE OF a, sub`, impersonation.ActorUserID, impersonation.SubjectUserID).
			Scan(&actorRole, &actorActive, &subjectRole, &subjectActive); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrImpersonationDenied
		} else if err != nil {
			return fmt.Errorf("load impersonation users: %w", err)
		}
		if !actorActive || actorRole != domain.PlatformRoleAdmin || !subjectActive ||
			subjectRole != domain.PlatformRoleMember || impersonation.ActorUserID == impersonation.SubjectUserID {
			return identity.ErrImpersonationDenied
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO impersonations (id, session_id, actor_user_id, subject_user_id, started_at, ended_at)
			VALUES ($1,$2,$3,$4,$5,NULL)`,
			impersonation.ID, impersonation.SessionID, impersonation.ActorUserID,
			impersonation.SubjectUserID, impersonation.StartedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return identity.ErrImpersonationActive
			}
			return fmt.Errorf("insert impersonation: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) EndImpersonation(ctx context.Context, sessionID uuid.UUID, endedAt time.Time, audit identity.AuditEvent) error {
	return s.inTransaction(ctx, "end impersonation", func(tx pgx.Tx) error {
		if err := lockSession(ctx, tx, sessionID); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM impersonations
			WHERE session_id=$1 AND ended_at IS NULL FOR UPDATE`, sessionID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrImpersonationNotFound
		} else if err != nil {
			return fmt.Errorf("lock impersonation: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE impersonations SET ended_at=$2 WHERE id=$1`, id, endedAt); err != nil {
			return fmt.Errorf("end impersonation: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) CurrentImpersonation(ctx context.Context, sessionID uuid.UUID) (*identity.Impersonation, error) {
	impersonation, _, err := s.currentImpersonation(ctx, sessionID)
	return impersonation, err
}

func (s *IdentityStore) RecordDelivery(ctx context.Context, delivery identity.InvitationDelivery) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO invitation_deliveries
			(id, invitation_id, channel, status, provider_reference, error_category, attempted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		delivery.ID, delivery.InvitationID, delivery.Channel, delivery.Status,
		delivery.ProviderReference, delivery.ErrorCategory, delivery.AttemptedAt)
	if err != nil {
		return fmt.Errorf("record invitation delivery: %w", err)
	}
	return nil
}

func (s *IdentityStore) AppendAudit(ctx context.Context, audit identity.AuditEvent) error {
	if err := insertAudit(ctx, s.db.Pool, audit); err != nil {
		return fmt.Errorf("append identity audit: %w", err)
	}
	return nil
}

func (s *IdentityStore) changeInvitation(
	ctx context.Context,
	operation string,
	id uuid.UUID,
	audit identity.AuditEvent,
	change func(pgx.Tx, identity.Invitation) error,
) error {
	return s.inTransaction(ctx, operation, func(tx pgx.Tx) error {
		invitation, err := scanInvitation(tx.QueryRow(ctx, `
			SELECT id, provider, tenant_id, target_subject_id, target_snapshot, token_hash, status,
			       created_by_user_id, expires_at, accepted_by_user_id, accepted_at, revoked_at, created_at, updated_at
			FROM invitations WHERE id=$1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrInvitationInvalid
		}
		if err != nil {
			return fmt.Errorf("lock invitation: %w", err)
		}
		if err := change(tx, invitation); err != nil {
			return err
		}
		return insertAudit(ctx, tx, audit)
	})
}

func (s *IdentityStore) lockSessionUpdate(ctx context.Context, sessionID uuid.UUID, query string, args ...any) error {
	return s.inTransaction(ctx, "update session security state", func(tx pgx.Tx) error {
		if err := lockSession(ctx, tx, sessionID); err != nil {
			return err
		}
		allArgs := append([]any{sessionID}, args...)
		if _, err := tx.Exec(ctx, query, allArgs...); err != nil {
			return fmt.Errorf("update session security state: %w", err)
		}
		return nil
	})
}

func (s *IdentityStore) inTransaction(ctx context.Context, operation string, fn func(pgx.Tx) error) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s: %w", operation, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

func (s *IdentityStore) currentImpersonation(ctx context.Context, sessionID uuid.UUID) (*identity.Impersonation, *domain.User, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT i.id, i.session_id, i.actor_user_id, i.subject_user_id, i.started_at, i.ended_at,
		       u.id, u.name, u.email, u.avatar_url, u.platform_role, u.roles, u.active, u.created_at, u.updated_at
		FROM impersonations i JOIN users u ON u.id=i.subject_user_id
		WHERE i.session_id=$1 AND i.ended_at IS NULL`, sessionID)
	var impersonation identity.Impersonation
	var subject domain.User
	var roles []string
	err := row.Scan(
		&impersonation.ID, &impersonation.SessionID, &impersonation.ActorUserID,
		&impersonation.SubjectUserID, &impersonation.StartedAt, &impersonation.EndedAt,
		&subject.ID, &subject.Name, &subject.Email, &subject.AvatarURL, &subject.PlatformRole,
		&roles, &subject.Active, &subject.CreatedAt, &subject.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load active impersonation: %w", err)
	}
	subject.Roles = userRoles(roles)
	return &impersonation, &subject, nil
}

func insertExternalIdentityAndCredential(
	ctx context.Context,
	tx pgx.Tx,
	externalID, userID uuid.UUID,
	principal identity.Principal,
	credential identity.OAuthCredential,
	now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO external_identities
			(id, user_id, provider, tenant_id, subject_id, provider_profile, last_verified_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
		externalID, userID, principal.Key.Provider, principal.Key.TenantID, principal.Key.SubjectID,
		jsonOrEmpty(principal.Profile), now, now)
	if err != nil {
		return fmt.Errorf("insert external identity: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO oauth_credentials
			(external_identity_id, access_token_ciphertext, refresh_token_ciphertext,
			 access_token_expires_at, refresh_token_expires_at, encryption_key_id, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		externalID, credential.AccessTokenCiphertext, credential.RefreshTokenCiphertext,
		credential.AccessTokenExpiresAt, credential.RefreshTokenExpiresAt,
		credential.EncryptionKeyID, now)
	if err != nil {
		return fmt.Errorf("insert credential: %w", err)
	}
	return nil
}

func insertSession(ctx context.Context, tx pgx.Tx, session identity.Session) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO sessions
			(id, user_id, secret_hash, csrf_secret_hash, created_at, last_seen_at,
			 idle_expires_at, absolute_expires_at, last_provider_verified_at,
			 provider_failure_since, revoked_at, revoke_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		session.ID, session.UserID, session.SecretHash, session.CSRFSecretHash,
		session.CreatedAt, session.LastSeenAt, session.IdleExpiresAt, session.AbsoluteExpiresAt,
		session.LastProviderVerifiedAt, session.ProviderFailureSince, session.RevokedAt, session.RevokeReason)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

type auditExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertAudit(ctx context.Context, execer auditExecer, audit identity.AuditEvent) error {
	if audit.ID == uuid.Nil {
		return errors.New("identity audit id is required")
	}
	_, err := execer.Exec(ctx, `
		INSERT INTO identity_audit_events
			(id, event_type, actor_user_id, subject_user_id, invitation_id,
			 session_id, request_id, metadata, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		audit.ID, audit.EventType, audit.ActorUserID, audit.SubjectUserID, audit.InvitationID,
		audit.SessionID, audit.RequestID, jsonOrEmpty(audit.Metadata), audit.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert identity audit: %w", err)
	}
	return nil
}

func lockSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrSessionInvalid
	} else if err != nil {
		return fmt.Errorf("lock session: %w", err)
	}
	return nil
}

func scanAuthorization(row pgx.Row) (identity.AuthorizationTransaction, error) {
	var transaction identity.AuthorizationTransaction
	err := row.Scan(&transaction.ID, &transaction.Purpose, &transaction.StateHash,
		&transaction.InvitationID, &transaction.ExpiresAt, &transaction.ConsumedAt, &transaction.CreatedAt)
	return transaction, err
}

func scanInvitation(row pgx.Row) (identity.Invitation, error) {
	var invitation identity.Invitation
	err := row.Scan(
		&invitation.ID, &invitation.Target.Provider, &invitation.Target.TenantID,
		&invitation.Target.SubjectID, &invitation.TargetSnapshot, &invitation.TokenHash,
		&invitation.Status, &invitation.CreatedByUserID, &invitation.ExpiresAt,
		&invitation.AcceptedByUserID, &invitation.AcceptedAt, &invitation.RevokedAt,
		&invitation.CreatedAt, &invitation.UpdatedAt,
	)
	return invitation, err
}

func scanCredential(row pgx.Row) (identity.OAuthCredential, error) {
	var credential identity.OAuthCredential
	err := row.Scan(&credential.AccessTokenCiphertext, &credential.RefreshTokenCiphertext,
		&credential.AccessTokenExpiresAt, &credential.RefreshTokenExpiresAt, &credential.EncryptionKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.OAuthCredential{}, identity.ErrCredentialNotFound
	}
	if err != nil {
		return identity.OAuthCredential{}, fmt.Errorf("scan credential: %w", err)
	}
	return credential, nil
}

func scanSession(row pgx.Row) (identity.Session, error) {
	var session identity.Session
	err := row.Scan(
		&session.ID, &session.UserID, &session.SecretHash, &session.CSRFSecretHash,
		&session.CreatedAt, &session.LastSeenAt, &session.IdleExpiresAt, &session.AbsoluteExpiresAt,
		&session.LastProviderVerifiedAt, &session.ProviderFailureSince, &session.RevokedAt, &session.RevokeReason,
	)
	return session, err
}

func jsonOrEmpty(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}

func userRoles(values []string) []domain.UserRole {
	roles := make([]domain.UserRole, len(values))
	for i, value := range values {
		roles[i] = domain.UserRole(value)
	}
	return roles
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
