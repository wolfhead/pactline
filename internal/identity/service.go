package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
)

type SessionRepository interface {
	CreateSession(ctx context.Context, session Session, audit AuditEvent) error
	ResolveSession(ctx context.Context, id uuid.UUID, secretHash []byte, now time.Time) (SessionBundle, error)
	TouchSession(ctx context.Context, id uuid.UUID, now, idleExpiresAt time.Time) (bool, error)
	LogoutSession(ctx context.Context, sessionID uuid.UUID, now time.Time, requestID string) error
}

type UserRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

type IdentityRepository interface {
	CreateAuthorizationTransaction(ctx context.Context, transaction AuthorizationTransaction) error
	ConsumeAuthorizationState(ctx context.Context, stateHash []byte, now time.Time) (AuthorizationTransaction, error)
	BootstrapAdmin(ctx context.Context, command BootstrapAdminCommand) (domain.User, error)
	LoginExternal(ctx context.Context, command LoginCommand) (domain.User, error)
	GetExternalIdentityForUser(ctx context.Context, userID uuid.UUID) (ExternalIdentity, error)
	RefreshCredentialLocked(ctx context.Context, externalIdentityID uuid.UUID, refresh func(OAuthCredential) (OAuthCredential, error)) (OAuthCredential, error)
	RecordProviderVerification(ctx context.Context, sessionID uuid.UUID, verifiedAt time.Time) error
	RecordProviderFailure(ctx context.Context, sessionID uuid.UUID, failedAt time.Time, audit AuditEvent) error
	DeactivateUser(ctx context.Context, userID, actorID uuid.UUID, reason string) error
	AcceptInvitation(ctx context.Context, command AcceptInvitationCommand) (domain.User, error)
	CreateInvitation(ctx context.Context, invitation Invitation, audit AuditEvent) error
	ListInvitations(ctx context.Context) ([]Invitation, error)
	ExpireInvitations(ctx context.Context, now time.Time) (int64, error)
	GetInvitation(ctx context.Context, id uuid.UUID) (Invitation, error)
	GetInvitationByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (Invitation, error)
	RotateInvitation(ctx context.Context, id, actorID uuid.UUID, tokenHash []byte, expiresAt time.Time) (Invitation, error)
	RevokeInvitation(ctx context.Context, id uuid.UUID, now time.Time, audit AuditEvent) error
	RecordDelivery(ctx context.Context, delivery InvitationDelivery) error
}

type LarkServiceConfig struct {
	Repository          IdentityRepository
	Authenticator       Authenticator
	Verifier            PrincipalVerifier
	TenantID            string
	RedirectURI         string
	BootstrapAdminEmail string
	Directory           DirectoryProvider
	Notifier            NotificationSender
	AppBaseURL          string
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type CryptoSecretGenerator struct{}

func (CryptoSecretGenerator) NewSecret() (string, error) {
	return NewOpaqueSecret()
}

type Service struct {
	sessions       SessionRepository
	users          UserRepository
	sessionSecret  []byte
	clock          Clock
	secrets        SecretGenerator
	identity       IdentityRepository
	authenticator  Authenticator
	verifier       PrincipalVerifier
	tenantID       string
	redirectURI    string
	bootstrapEmail string
	directory      DirectoryProvider
	notifier       NotificationSender
	appBaseURL     string
}

func NewService(
	sessions SessionRepository,
	users UserRepository,
	sessionSecret []byte,
	clock Clock,
	secrets SecretGenerator,
) (*Service, error) {
	if len(sessionSecret) != 32 {
		return nil, errors.New("session envelope secret must contain exactly 32 bytes")
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if secrets == nil {
		secrets = CryptoSecretGenerator{}
	}
	return &Service{
		sessions: sessions, users: users, sessionSecret: append([]byte(nil), sessionSecret...),
		clock: clock, secrets: secrets,
	}, nil
}

func (s *Service) ConfigureLark(config LarkServiceConfig) error {
	if config.Repository == nil || config.Authenticator == nil || config.Verifier == nil ||
		config.TenantID == "" || config.RedirectURI == "" || strings.TrimSpace(config.BootstrapAdminEmail) == "" {
		return errors.New("complete Lark service configuration is required")
	}
	s.identity = config.Repository
	s.authenticator = config.Authenticator
	s.verifier = config.Verifier
	s.tenantID = config.TenantID
	s.redirectURI = config.RedirectURI
	s.bootstrapEmail = strings.TrimSpace(config.BootstrapAdminEmail)
	s.directory = config.Directory
	s.notifier = config.Notifier
	s.appBaseURL = strings.TrimRight(config.AppBaseURL, "/")
	return nil
}

type InvitationResult struct {
	Invitation Invitation
	Delivery   InvitationDelivery
}

func (s *Service) SearchDirectory(ctx context.Context, actor domain.User, query string) ([]Principal, error) {
	query = strings.TrimSpace(query)
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return nil, ErrAdminRequired
	}
	if len([]rune(query)) < 2 {
		return nil, domain.ErrInvalidInput
	}
	if s.directory == nil {
		return nil, ErrProviderContract
	}
	external, err := s.identity.GetExternalIdentityForUser(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("load administrator credential: %w", err)
	}
	principals, err := s.directory.SearchPrincipals(ctx, external.EncryptedCredential, query, 20)
	if err != nil {
		return nil, err
	}
	if len(principals) > 20 {
		principals = principals[:20]
	}
	return principals, nil
}

func (s *Service) ListInvitations(ctx context.Context, actor domain.User) ([]Invitation, error) {
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return nil, ErrAdminRequired
	}
	if _, err := s.identity.ExpireInvitations(ctx, s.clock.Now()); err != nil {
		return nil, err
	}
	return s.identity.ListInvitations(ctx)
}

func (s *Service) CreateInvitation(ctx context.Context, actor domain.User, subjectID, requestID string) (InvitationResult, error) {
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return InvitationResult{}, ErrAdminRequired
	}
	if s.directory == nil || s.notifier == nil || strings.TrimSpace(subjectID) == "" {
		return InvitationResult{}, domain.ErrInvalidInput
	}
	external, err := s.identity.GetExternalIdentityForUser(ctx, actor.ID)
	if err != nil {
		return InvitationResult{}, fmt.Errorf("load administrator credential: %w", err)
	}
	principal, err := s.directory.GetPrincipal(ctx, external.EncryptedCredential, strings.TrimSpace(subjectID))
	if err != nil {
		return InvitationResult{}, err
	}
	if principal.Key.Provider != "lark" || principal.Key.TenantID != s.tenantID ||
		principal.Key.SubjectID != strings.TrimSpace(subjectID) || !principal.Active {
		return InvitationResult{}, ErrInvitationInvalid
	}
	if _, _, err := s.findLoginUser(ctx, principal.Key); err == nil {
		return InvitationResult{}, ErrInvitationConflict
	} else if !errors.Is(err, domain.ErrNotFound) {
		return InvitationResult{}, err
	}
	rawToken, err := s.secrets.NewSecret()
	if err != nil {
		return InvitationResult{}, fmt.Errorf("generate invitation token: %w", err)
	}
	tokenHash := HashSecret([]byte(rawToken))
	now := s.clock.Now()
	snapshot, _ := json.Marshal(map[string]any{
		"name": principal.Name, "email": principal.Email, "avatar_url": principal.AvatarURL,
	})
	invitation := Invitation{
		ID: uuid.New(), Target: principal.Key, TargetSnapshot: snapshot, TokenHash: tokenHash[:],
		Status: InvitationPending, CreatedByUserID: actor.ID, ExpiresAt: InvitationExpiresAt(now),
		CreatedAt: now, UpdatedAt: now,
	}
	audit := AuditEvent{
		ID: uuid.New(), EventType: "invitation_created", ActorUserID: &actor.ID,
		InvitationID: &invitation.ID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	if requestID != "" {
		audit.RequestID = &requestID
	}
	if err := s.identity.CreateInvitation(ctx, invitation, audit); err != nil {
		return InvitationResult{}, err
	}
	delivery, err := s.deliverInvitation(ctx, invitation, rawToken)
	if err != nil {
		return InvitationResult{}, err
	}
	return InvitationResult{Invitation: invitation, Delivery: delivery}, nil
}

func (s *Service) ResendInvitation(ctx context.Context, actor domain.User, id uuid.UUID) (InvitationResult, error) {
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return InvitationResult{}, ErrAdminRequired
	}
	rawToken, err := s.secrets.NewSecret()
	if err != nil {
		return InvitationResult{}, fmt.Errorf("generate invitation token: %w", err)
	}
	hash := HashSecret([]byte(rawToken))
	invitation, err := s.identity.RotateInvitation(ctx, id, actor.ID, hash[:], InvitationExpiresAt(s.clock.Now()))
	if err != nil {
		return InvitationResult{}, err
	}
	delivery, err := s.deliverInvitation(ctx, invitation, rawToken)
	if err != nil {
		return InvitationResult{}, err
	}
	return InvitationResult{Invitation: invitation, Delivery: delivery}, nil
}

func (s *Service) RotateInvitationLink(ctx context.Context, actor domain.User, id uuid.UUID) (string, error) {
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return "", ErrAdminRequired
	}
	rawToken, err := s.secrets.NewSecret()
	if err != nil {
		return "", fmt.Errorf("generate invitation token: %w", err)
	}
	hash := HashSecret([]byte(rawToken))
	invitation, err := s.identity.RotateInvitation(ctx, id, actor.ID, hash[:], InvitationExpiresAt(s.clock.Now()))
	if err != nil {
		return "", err
	}
	delivery := InvitationDelivery{
		ID: uuid.New(), InvitationID: invitation.ID, Channel: DeliveryCopiedLink,
		Status: DeliveryDelivered, AttemptedAt: s.clock.Now(),
	}
	if err := s.identity.RecordDelivery(ctx, delivery); err != nil {
		return "", fmt.Errorf("record copied invitation link: %w", err)
	}
	return s.invitationURL(rawToken), nil
}

func (s *Service) RevokeInvitation(ctx context.Context, actor domain.User, id uuid.UUID, requestID string) error {
	if actor.PlatformRole != domain.PlatformRoleAdmin || !actor.Active {
		return ErrAdminRequired
	}
	now := s.clock.Now()
	audit := AuditEvent{
		ID: uuid.New(), EventType: "invitation_revoked", ActorUserID: &actor.ID,
		InvitationID: &id, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	if requestID != "" {
		audit.RequestID = &requestID
	}
	return s.identity.RevokeInvitation(ctx, id, now, audit)
}

func (s *Service) AcceptInvitationToken(ctx context.Context, token string) (AuthorizationStart, error) {
	if strings.TrimSpace(token) == "" {
		return AuthorizationStart{}, ErrInvitationInvalid
	}
	hash := HashSecret([]byte(token))
	invitation, err := s.identity.GetInvitationByTokenHash(ctx, hash[:], s.clock.Now())
	if err != nil {
		return AuthorizationStart{}, ErrInvitationInvalid
	}
	return s.StartAuthorization(ctx, AuthorizationInvitation, &invitation.ID)
}

func (s *Service) deliverInvitation(ctx context.Context, invitation Invitation, rawToken string) (InvitationDelivery, error) {
	delivery := InvitationDelivery{
		ID: uuid.New(), InvitationID: invitation.ID, Channel: DeliveryProviderDM,
		Status: DeliveryDelivered, AttemptedAt: s.clock.Now(),
	}
	receipt, err := s.notifier.SendInvitation(ctx, invitation.Target, s.invitationURL(rawToken))
	if err != nil {
		delivery.Status = DeliveryFailed
		category := ProviderUnavailable
		delivery.ErrorCategory = &category
	} else {
		delivery.ProviderReference = &receipt.ProviderReference
	}
	if recordErr := s.identity.RecordDelivery(ctx, delivery); recordErr != nil {
		slog.Error("record invitation delivery failed",
			"invitation_id", invitation.ID, "status", delivery.Status, "error", recordErr)
		return InvitationDelivery{}, fmt.Errorf("record invitation delivery: %w", recordErr)
	}
	return delivery, nil
}

func (s *Service) invitationURL(rawToken string) string {
	return s.appBaseURL + "/invite#" + rawToken
}

func (s *Service) StartAuthorization(ctx context.Context, purpose AuthorizationPurpose, invitationID *uuid.UUID) (AuthorizationStart, error) {
	if s.identity == nil || s.authenticator == nil {
		return AuthorizationStart{}, ErrLoginDenied
	}
	if purpose == AuthorizationLogin && invitationID != nil ||
		purpose == AuthorizationInvitation && invitationID == nil {
		return AuthorizationStart{}, ErrAuthorizationInvalid
	}
	state, err := s.secrets.NewSecret()
	if err != nil {
		return AuthorizationStart{}, fmt.Errorf("generate authorization state: %w", err)
	}
	stateHash := HashSecret([]byte(state))
	now := s.clock.Now()
	transaction := AuthorizationTransaction{
		ID: uuid.New(), Purpose: purpose, StateHash: stateHash[:], InvitationID: invitationID,
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	}
	if err := s.identity.CreateAuthorizationTransaction(ctx, transaction); err != nil {
		return AuthorizationStart{}, fmt.Errorf("persist authorization state: %w", err)
	}
	start, err := s.authenticator.StartAuthorization(ctx, AuthorizationRequest{State: state, RedirectURI: s.redirectURI})
	if err != nil {
		return AuthorizationStart{}, fmt.Errorf("start provider authorization: %w", err)
	}
	return start, nil
}

func (s *Service) CompleteAuthorization(ctx context.Context, state, code, requestID string) (SessionTokens, error) {
	if s.identity == nil || s.authenticator == nil || state == "" || code == "" {
		return SessionTokens{}, ErrAuthorizationInvalid
	}
	stateHash := HashSecret([]byte(state))
	transaction, err := s.identity.ConsumeAuthorizationState(ctx, stateHash[:], s.clock.Now())
	if err != nil {
		return SessionTokens{}, ErrAuthorizationInvalid
	}
	authenticated, err := s.authenticator.ExchangeAuthorizationCode(ctx, code)
	if err != nil {
		return SessionTokens{}, ErrLoginDenied
	}
	if authenticated.Principal.Key.Provider != "lark" ||
		authenticated.Principal.Key.TenantID != s.tenantID || !authenticated.Principal.Active {
		return SessionTokens{}, ErrLoginDenied
	}
	tokens, session, err := s.newSession(uuid.Nil)
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.clock.Now()
	audit := AuditEvent{
		ID: uuid.New(), EventType: "login_succeeded", SessionID: &session.ID,
		Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	if requestID != "" {
		audit.RequestID = &requestID
	}
	switch transaction.Purpose {
	case AuthorizationInvitation:
		if transaction.InvitationID == nil {
			return SessionTokens{}, ErrLoginDenied
		}
		userID := uuid.New()
		session.UserID = userID
		audit.EventType = "invitation_accepted"
		_, err = s.identity.AcceptInvitation(ctx, AcceptInvitationCommand{
			InvitationID: *transaction.InvitationID,
			Principal:    authenticated.Principal, Credential: authenticated.Credential,
			UserID: userID, UserName: authenticated.Principal.Name,
			UserEmail: authenticated.Principal.Email, UserAvatarURL: authenticated.Principal.AvatarURL,
			Session: session, Audit: audit, Now: now,
		})
		if err != nil {
			return SessionTokens{}, ErrLoginDenied
		}
		return tokens, nil
	case AuthorizationLogin:
	default:
		return SessionTokens{}, ErrLoginDenied
	}

	session.UserID = uuid.Nil
	_, user, findErr := s.findLoginUser(ctx, authenticated.Principal.Key)
	if findErr == nil {
		session.UserID = user.ID
		_, err = s.identity.LoginExternal(ctx, LoginCommand{
			Principal: authenticated.Principal, Credential: authenticated.Credential,
			Session: session, Audit: audit, Now: now,
		})
		if err != nil {
			return SessionTokens{}, ErrLoginDenied
		}
		return tokens, nil
	}
	if !errors.Is(findErr, domain.ErrNotFound) {
		return SessionTokens{}, fmt.Errorf("lookup external login: %w", findErr)
	}
	emailMatches := authenticated.Principal.Email != nil && authenticated.Principal.EmailVerified &&
		strings.EqualFold(strings.TrimSpace(*authenticated.Principal.Email), s.bootstrapEmail)
	if !emailMatches {
		return SessionTokens{}, ErrLoginDenied
	}
	primarySeed := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	session.UserID = primarySeed
	audit.EventType = "administrator_bootstrapped"
	if _, err := s.identity.BootstrapAdmin(ctx, BootstrapAdminCommand{
		Principal: authenticated.Principal, Credential: authenticated.Credential,
		Session: session, Audit: audit, Now: now,
	}); err != nil {
		return SessionTokens{}, ErrLoginDenied
	}
	return tokens, nil
}

func (s *Service) findLoginUser(ctx context.Context, key PrincipalKey) (ExternalIdentity, domain.User, error) {
	type externalFinder interface {
		FindExternalIdentity(context.Context, PrincipalKey) (ExternalIdentity, domain.User, error)
	}
	finder, ok := s.identity.(externalFinder)
	if !ok {
		return ExternalIdentity{}, domain.User{}, domain.ErrNotFound
	}
	return finder.FindExternalIdentity(ctx, key)
}

func (s *Service) IssueSession(ctx context.Context, userID uuid.UUID, requestID string) (SessionTokens, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return SessionTokens{}, ErrSessionInvalid
		}
		return SessionTokens{}, fmt.Errorf("load session user: %w", err)
	}
	if !user.Active {
		return SessionTokens{}, ErrUserInactive
	}
	tokens, session, err := s.newSession(userID)
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.clock.Now()
	audit := AuditEvent{
		ID: uuid.New(), EventType: "session_created", SubjectUserID: &userID,
		SessionID: &session.ID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	if requestID != "" {
		audit.RequestID = &requestID
	}
	if err := s.sessions.CreateSession(ctx, session, audit); err != nil {
		return SessionTokens{}, fmt.Errorf("persist session: %w", err)
	}
	return tokens, nil
}

func (s *Service) newSession(userID uuid.UUID) (SessionTokens, Session, error) {
	sessionSecret, err := s.secrets.NewSecret()
	if err != nil {
		return SessionTokens{}, Session{}, fmt.Errorf("generate session secret: %w", err)
	}
	csrfSecret, err := s.secrets.NewSecret()
	if err != nil {
		return SessionTokens{}, Session{}, fmt.Errorf("generate csrf secret: %w", err)
	}
	now := s.clock.Now()
	idleExpiresAt, absoluteExpiresAt := NewSessionTimes(now)
	sessionHash, csrfHash := HashSecret([]byte(sessionSecret)), HashSecret([]byte(csrfSecret))
	sessionID := uuid.New()
	session := Session{
		ID: sessionID, UserID: userID, SecretHash: sessionHash[:], CSRFSecretHash: csrfHash[:],
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
	}
	return SessionTokens{SessionID: sessionID, SessionSecret: sessionSecret, CSRFSecret: csrfSecret}, session, nil
}

func (s *Service) SessionCookieValue(tokens SessionTokens) string {
	envelope := tokens.SessionID.String() + "." + tokens.SessionSecret
	return envelope + "." + s.signEnvelope(envelope)
}

func (s *Service) Authenticate(ctx context.Context, cookieValue string) (RequestIdentity, Session, error) {
	sessionID, secretHash, err := s.parseSessionCookie(cookieValue)
	if err != nil {
		return RequestIdentity{}, Session{}, err
	}
	now := s.clock.Now()
	bundle, err := s.sessions.ResolveSession(ctx, sessionID, secretHash, now)
	if err != nil {
		return RequestIdentity{}, Session{}, err
	}
	if s.identity != nil && s.verifier != nil && ProviderVerificationDue(bundle.Session.LastProviderVerifiedAt, now) {
		if err := s.revalidate(ctx, &bundle, now); err != nil {
			return RequestIdentity{}, Session{}, err
		}
	}
	requestIdentity := RequestIdentity{
		SessionID: sessionID, Actor: bundle.User, Subject: bundle.User, Impersonation: bundle.Impersonation,
	}
	if bundle.Impersonation != nil {
		if bundle.Subject == nil || !bundle.Subject.Active {
			return RequestIdentity{}, Session{}, ErrSessionInvalid
		}
		requestIdentity.Subject = *bundle.Subject
	}
	if SessionRollDue(bundle.Session, now) {
		nextExpiry := RollingIdleExpiry(now, bundle.Session.AbsoluteExpiresAt)
		if _, err := s.sessions.TouchSession(ctx, sessionID, now, nextExpiry); err != nil {
			return RequestIdentity{}, Session{}, fmt.Errorf("roll session activity: %w", err)
		}
		bundle.Session.LastSeenAt = now
		bundle.Session.IdleExpiresAt = nextExpiry
	}
	return requestIdentity, bundle.Session, nil
}

func (s *Service) revalidate(ctx context.Context, bundle *SessionBundle, now time.Time) error {
	external, err := s.identity.GetExternalIdentityForUser(ctx, bundle.User.ID)
	if err != nil {
		return ErrSessionInvalid
	}
	credential := external.EncryptedCredential
	if !now.Before(credential.AccessTokenExpiresAt) {
		if !now.Before(credential.RefreshTokenExpiresAt) {
			return ErrSessionInvalid
		}
		credential, err = s.identity.RefreshCredentialLocked(ctx, external.ID, func(current OAuthCredential) (OAuthCredential, error) {
			refreshed, refreshErr := s.authenticator.RefreshCredential(ctx, current)
			return refreshed.Credential, refreshErr
		})
		if err != nil {
			return ErrSessionInvalid
		}
	}
	result, err := s.verifier.VerifyPrincipal(ctx, credential, external.Key)
	if err != nil {
		result = VerificationResult{State: VerificationTransient, Category: ProviderUnavailable}
	}
	switch result.State {
	case VerificationValid:
		if err := s.identity.RecordProviderVerification(ctx, bundle.Session.ID, now); err != nil {
			return fmt.Errorf("record provider verification: %w", err)
		}
		bundle.Session.LastProviderVerifiedAt = &now
		bundle.Session.ProviderFailureSince = nil
		return nil
	case VerificationInvalid:
		if !IsExplicitInvalid(result.Category) {
			return ErrProviderContract
		}
		if err := s.identity.DeactivateUser(ctx, bundle.User.ID, bundle.User.ID, string(result.Category)); err != nil {
			return fmt.Errorf("deactivate invalid provider principal: %w", err)
		}
		return ErrUserInactive
	case VerificationTransient:
		audit := AuditEvent{
			ID: uuid.New(), EventType: "provider_verification_failed", SubjectUserID: &bundle.User.ID,
			SessionID: &bundle.Session.ID, Metadata: json.RawMessage(fmt.Sprintf(`{"category":%q}`, result.Category)),
			OccurredAt: now,
		}
		if err := s.identity.RecordProviderFailure(ctx, bundle.Session.ID, now, audit); err != nil {
			return fmt.Errorf("record provider failure: %w", err)
		}
		if bundle.Session.ProviderFailureSince == nil {
			bundle.Session.ProviderFailureSince = &now
			return nil
		}
		if WithinProviderGrace(bundle.Session.ProviderFailureSince, now) {
			return nil
		}
		return ErrProviderTransient
	default:
		return ErrProviderContract
	}
}

func (s *Service) VerifyCSRF(session Session, token string) bool {
	if token == "" {
		return false
	}
	actual := HashSecret([]byte(token))
	return len(session.CSRFSecretHash) == sha256.Size &&
		subtle.ConstantTimeCompare(actual[:], session.CSRFSecretHash) == 1
}

func (s *Service) Logout(ctx context.Context, requestIdentity RequestIdentity, requestID string) error {
	if err := s.sessions.LogoutSession(ctx, requestIdentity.SessionID, s.clock.Now(), requestID); err != nil {
		return fmt.Errorf("persist logout: %w", err)
	}
	return nil
}

func (s *Service) parseSessionCookie(value string) (uuid.UUID, []byte, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return uuid.Nil, nil, ErrSessionInvalid
	}
	envelope := parts[0] + "." + parts[1]
	expectedMAC := s.signEnvelope(envelope)
	if len(parts[2]) != len(expectedMAC) ||
		subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expectedMAC)) != 1 {
		return uuid.Nil, nil, ErrSessionInvalid
	}
	sessionID, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, nil, ErrSessionInvalid
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) != 32 {
		return uuid.Nil, nil, ErrSessionInvalid
	}
	hash := HashSecret([]byte(parts[1]))
	return sessionID, hash[:], nil
}

func (s *Service) signEnvelope(envelope string) string {
	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write([]byte(envelope))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
