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

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type CryptoSecretGenerator struct{}

func (CryptoSecretGenerator) NewSecret() (string, error) {
	return NewOpaqueSecret()
}

type Service struct {
	sessions      SessionRepository
	users         UserRepository
	sessionSecret []byte
	clock         Clock
	secrets       SecretGenerator
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
	sessionSecret, err := s.secrets.NewSecret()
	if err != nil {
		return SessionTokens{}, fmt.Errorf("generate session secret: %w", err)
	}
	csrfSecret, err := s.secrets.NewSecret()
	if err != nil {
		return SessionTokens{}, fmt.Errorf("generate csrf secret: %w", err)
	}
	now := s.clock.Now()
	idleExpiresAt, absoluteExpiresAt := NewSessionTimes(now)
	sessionHash, csrfHash := HashSecret([]byte(sessionSecret)), HashSecret([]byte(csrfSecret))
	sessionID := uuid.New()
	session := Session{
		ID: sessionID, UserID: userID, SecretHash: sessionHash[:], CSRFSecretHash: csrfHash[:],
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
	}
	audit := AuditEvent{
		ID: uuid.New(), EventType: "session_created", SubjectUserID: &userID,
		SessionID: &sessionID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
	if requestID != "" {
		audit.RequestID = &requestID
	}
	if err := s.sessions.CreateSession(ctx, session, audit); err != nil {
		return SessionTokens{}, fmt.Errorf("persist session: %w", err)
	}
	return SessionTokens{SessionID: sessionID, SessionSecret: sessionSecret, CSRFSecret: csrfSecret}, nil
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
