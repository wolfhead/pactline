package identity

import (
	"context"
	"time"
)

type AuthorizationRequest struct {
	State       string
	RedirectURI string
}

type AuthorizationStart struct {
	URL string
}

type AuthenticatedPrincipal struct {
	Principal  Principal
	Credential OAuthCredential
}

type RefreshedCredential struct {
	Credential OAuthCredential
}

type DeliveryReceipt struct {
	ProviderReference string
	RequestID         string
}

type Authenticator interface {
	StartAuthorization(ctx context.Context, request AuthorizationRequest) (AuthorizationStart, error)
	ExchangeAuthorizationCode(ctx context.Context, code string) (AuthenticatedPrincipal, error)
	RefreshCredential(ctx context.Context, credential OAuthCredential) (RefreshedCredential, error)
}

type DirectoryProvider interface {
	SearchPrincipals(ctx context.Context, credential OAuthCredential, query string, limit int) ([]Principal, error)
	GetPrincipal(ctx context.Context, credential OAuthCredential, subjectID string) (Principal, error)
}

type PrincipalVerifier interface {
	VerifyPrincipal(ctx context.Context, credential OAuthCredential, expected PrincipalKey) (VerificationResult, error)
}

type NotificationSender interface {
	SendInvitation(ctx context.Context, recipient PrincipalKey, invitationURL string) (DeliveryReceipt, error)
}

type Clock interface {
	Now() time.Time
}

type SecretGenerator interface {
	NewSecret() (string, error)
}
