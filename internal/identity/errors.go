package identity

import (
	"errors"
)

var (
	ErrAuthorizationInvalid  = errors.New("authorization transaction is invalid")
	ErrInvitationInvalid     = errors.New("invitation is invalid")
	ErrInvitationConflict    = errors.New("a pending invitation already exists")
	ErrCredentialNotFound    = errors.New("credential not found")
	ErrSessionInvalid        = errors.New("session is invalid")
	ErrSessionExpired        = errors.New("session expired")
	ErrSessionRevoked        = errors.New("session revoked")
	ErrUserInactive          = errors.New("user is inactive")
	ErrImpersonationDenied   = errors.New("impersonation denied")
	ErrImpersonationActive   = errors.New("impersonation already active")
	ErrImpersonationNotFound = errors.New("active impersonation not found")
	ErrProviderTransient     = errors.New("provider verification is temporarily unavailable")
	ErrProviderContract      = errors.New("provider response is invalid")
	ErrLoginDenied           = errors.New("login denied")
	ErrAdminRequired         = errors.New("administrator access required")
)

type CategorizedProviderError interface {
	error
	ProviderCategory() ProviderErrorCategory
}

func ProviderCategoryFromError(err error) (ProviderErrorCategory, bool) {
	var categorized CategorizedProviderError
	if !errors.As(err, &categorized) {
		return "", false
	}
	return categorized.ProviderCategory(), true
}

type ProviderRequestIdentifiedError interface {
	error
	ProviderRequestID() string
}

func ProviderRequestIDFromError(err error) string {
	var identified ProviderRequestIdentifiedError
	if !errors.As(err, &identified) {
		return ""
	}
	return identified.ProviderRequestID()
}
