package identity

import (
	"encoding/json"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
)

type PrincipalKey struct {
	Provider  string
	TenantID  string
	SubjectID string
}

type Principal struct {
	Key           PrincipalKey
	Name          string
	Email         *string
	EmailVerified bool
	AvatarURL     *string
	Active        bool
	Profile       json.RawMessage
}

type OAuthCredential struct {
	AccessTokenCiphertext  []byte
	RefreshTokenCiphertext []byte
	AccessTokenExpiresAt   time.Time
	RefreshTokenExpiresAt  time.Time
	EncryptionKeyID        string
}

type ExternalIdentity struct {
	ID                  uuid.UUID
	UserID              uuid.UUID
	Key                 PrincipalKey
	ProviderProfile     json.RawMessage
	LastVerifiedAt      *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	EncryptedCredential OAuthCredential
}

type InvitationStatus string

const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationRevoked  InvitationStatus = "revoked"
	InvitationExpired  InvitationStatus = "expired"
)

type Invitation struct {
	ID               uuid.UUID
	Target           PrincipalKey
	TargetSnapshot   json.RawMessage
	TokenHash        []byte
	Status           InvitationStatus
	CreatedByUserID  uuid.UUID
	ExpiresAt        time.Time
	AcceptedByUserID *uuid.UUID
	AcceptedAt       *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DeliveryChannel string

const (
	DeliveryProviderDM DeliveryChannel = "provider_dm"
	DeliveryCopiedLink DeliveryChannel = "copied_link"
)

type DeliveryStatus string

const (
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

type InvitationDelivery struct {
	ID                uuid.UUID
	InvitationID      uuid.UUID
	Channel           DeliveryChannel
	Status            DeliveryStatus
	ProviderReference *string
	ErrorCategory     *ProviderErrorCategory
	AttemptedAt       time.Time
}

type AuthorizationPurpose string

const (
	AuthorizationLogin      AuthorizationPurpose = "login"
	AuthorizationInvitation AuthorizationPurpose = "invitation"
)

type AuthorizationTransaction struct {
	ID           uuid.UUID
	Purpose      AuthorizationPurpose
	StateHash    []byte
	InvitationID *uuid.UUID
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	CreatedAt    time.Time
}

type Session struct {
	ID                     uuid.UUID
	UserID                 uuid.UUID
	SecretHash             []byte
	CSRFSecretHash         []byte
	CreatedAt              time.Time
	LastSeenAt             time.Time
	IdleExpiresAt          time.Time
	AbsoluteExpiresAt      time.Time
	LastProviderVerifiedAt *time.Time
	ProviderFailureSince   *time.Time
	RevokedAt              *time.Time
	RevokeReason           *string
}

type SessionBundle struct {
	Session       Session
	User          domain.User
	External      *ExternalIdentity
	Impersonation *Impersonation
	Subject       *domain.User
}

type SessionTokens struct {
	SessionID     uuid.UUID
	SessionSecret string
	CSRFSecret    string
}

type Impersonation struct {
	ID            uuid.UUID
	SessionID     uuid.UUID
	ActorUserID   uuid.UUID
	SubjectUserID uuid.UUID
	StartedAt     time.Time
	EndedAt       *time.Time
}

type AuditEvent struct {
	ID            uuid.UUID
	EventType     string
	ActorUserID   *uuid.UUID
	SubjectUserID *uuid.UUID
	InvitationID  *uuid.UUID
	SessionID     *uuid.UUID
	RequestID     *string
	Metadata      json.RawMessage
	OccurredAt    time.Time
}

type VerificationState string

const (
	VerificationValid     VerificationState = "valid"
	VerificationInvalid   VerificationState = "invalid"
	VerificationTransient VerificationState = "transient"
)

type ProviderErrorCategory string

const (
	ProviderUnauthorized         ProviderErrorCategory = "unauthorized"
	ProviderNotFound             ProviderErrorCategory = "not_found"
	ProviderResigned             ProviderErrorCategory = "resigned"
	ProviderFrozen               ProviderErrorCategory = "frozen"
	ProviderAuthorizationRevoked ProviderErrorCategory = "authorization_revoked"
	ProviderCredentialExpired    ProviderErrorCategory = "credential_expired"
	ProviderRateLimited          ProviderErrorCategory = "rate_limited"
	ProviderUnavailable          ProviderErrorCategory = "unavailable"
	ProviderContract             ProviderErrorCategory = "contract"
)

type VerificationResult struct {
	State     VerificationState
	Category  ProviderErrorCategory
	Principal *Principal
	RequestID string
}

type AcceptInvitationCommand struct {
	TokenHash     []byte
	Principal     Principal
	Credential    OAuthCredential
	UserID        uuid.UUID
	UserName      string
	UserEmail     *string
	UserAvatarURL *string
	Session       Session
	Audit         AuditEvent
	Now           time.Time
}
