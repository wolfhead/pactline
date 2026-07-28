package identity

import (
	"net/http"
	"time"

	"bountyboard/internal/domain"
)

const (
	InvitationLifetime           = 7 * 24 * time.Hour
	SessionIdleLifetime          = 24 * time.Hour
	SessionAbsoluteLifetime      = 30 * 24 * time.Hour
	ProviderVerificationInterval = 15 * time.Minute
	ProviderTransientGrace       = time.Hour
	SessionRollingWriteInterval  = 5 * time.Minute
)

func InvitationExpiresAt(createdAt time.Time) time.Time {
	return createdAt.Add(InvitationLifetime)
}

func InvitationMatches(invitation Invitation, key PrincipalKey, now time.Time) bool {
	return invitation.Status == InvitationPending &&
		now.Before(invitation.ExpiresAt) &&
		invitation.Target == key
}

func NewSessionTimes(now time.Time) (idleExpiresAt, absoluteExpiresAt time.Time) {
	absoluteExpiresAt = now.Add(SessionAbsoluteLifetime)
	return now.Add(SessionIdleLifetime), absoluteExpiresAt
}

func RollingIdleExpiry(now, absoluteExpiresAt time.Time) time.Time {
	next := now.Add(SessionIdleLifetime)
	if next.After(absoluteExpiresAt) {
		return absoluteExpiresAt
	}
	return next
}

func SessionUsable(session Session, now time.Time) bool {
	return session.RevokedAt == nil && now.Before(session.IdleExpiresAt) && now.Before(session.AbsoluteExpiresAt)
}

func SessionRollDue(session Session, now time.Time) bool {
	return SessionUsable(session, now) && !now.Before(session.LastSeenAt.Add(SessionRollingWriteInterval))
}

func ProviderVerificationDue(lastVerifiedAt *time.Time, now time.Time) bool {
	return lastVerifiedAt == nil || !now.Before(lastVerifiedAt.Add(ProviderVerificationInterval))
}

func WithinProviderGrace(failureSince *time.Time, now time.Time) bool {
	return failureSince != nil && !now.After(failureSince.Add(ProviderTransientGrace))
}

func IsExplicitInvalid(category ProviderErrorCategory) bool {
	switch category {
	case ProviderUnauthorized, ProviderNotFound, ProviderResigned, ProviderFrozen, ProviderAuthorizationRevoked:
		return true
	default:
		return false
	}
}

func CanImpersonate(actor, subject domain.User) bool {
	return actor.Active &&
		actor.PlatformRole == domain.PlatformRoleAdmin &&
		subject.Active &&
		subject.PlatformRole == domain.PlatformRoleMember &&
		actor.ID != subject.ID
}

func RequestAllowedDuringImpersonation(method, route string) bool {
	if method == http.MethodDelete && route == "/api/admin/impersonation" {
		return true
	}
	if method == http.MethodPost && route == "/api/auth/logout" {
		return true
	}
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// WriteAllowed is kept as the policy entry point used by the accepted plan.
// Its result describes whether the request may run, including safe methods.
func WriteAllowed(method, route string, impersonating bool) bool {
	return !impersonating || RequestAllowedDuringImpersonation(method, route)
}
