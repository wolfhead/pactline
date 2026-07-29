package identity

import (
	"net/http"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestInvitationPolicy(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	key := PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "subject"}
	invitation := Invitation{Target: key, Status: InvitationPending, ExpiresAt: InvitationExpiresAt(now)}

	assert.Equal(t, now.Add(7*24*time.Hour), invitation.ExpiresAt)
	assert.True(t, InvitationMatches(invitation, key, invitation.ExpiresAt.Add(-time.Nanosecond)))
	assert.False(t, InvitationMatches(invitation, key, invitation.ExpiresAt))
	for _, mismatch := range []PrincipalKey{
		{Provider: "other", TenantID: "tenant", SubjectID: "subject"},
		{Provider: "lark", TenantID: "other", SubjectID: "subject"},
		{Provider: "lark", TenantID: "tenant", SubjectID: "other"},
	} {
		assert.False(t, InvitationMatches(invitation, mismatch, now))
	}
}

func TestSessionPolicy(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	idle, absolute := NewSessionTimes(now)
	assert.Equal(t, now.Add(24*time.Hour), idle)
	assert.Equal(t, now.Add(30*24*time.Hour), absolute)
	assert.Equal(t, absolute, RollingIdleExpiry(absolute.Add(-time.Hour), absolute))
	assert.Equal(t, now.Add(25*time.Hour), RollingIdleExpiry(now.Add(time.Hour), absolute))

	lastVerified := now.Add(-15 * time.Minute)
	assert.True(t, ProviderVerificationDue(&lastVerified, now))
	lastVerified = lastVerified.Add(time.Nanosecond)
	assert.False(t, ProviderVerificationDue(&lastVerified, now))

	failure := now.Add(-time.Hour)
	assert.True(t, WithinProviderGrace(&failure, now))
	assert.False(t, WithinProviderGrace(&failure, now.Add(time.Nanosecond)))
}

func TestExplicitProviderInvalidClassifications(t *testing.T) {
	for _, category := range []ProviderErrorCategory{
		ProviderUnauthorized, ProviderNotFound, ProviderResigned, ProviderFrozen, ProviderAuthorizationRevoked,
	} {
		assert.True(t, IsExplicitInvalid(category), category)
	}
	for _, category := range []ProviderErrorCategory{
		ProviderCredentialExpired, ProviderRateLimited, ProviderUnavailable, ProviderContract,
	} {
		assert.False(t, IsExplicitInvalid(category), category)
	}
}

func TestImpersonationPolicy(t *testing.T) {
	adminID, memberID := uuid.New(), uuid.New()
	admin := domain.User{ID: adminID, Active: true, PlatformRole: domain.PlatformRoleAdmin}
	member := domain.User{ID: memberID, Active: true, PlatformRole: domain.PlatformRoleMember}
	assert.True(t, CanImpersonate(admin, member))
	assert.False(t, CanImpersonate(admin, admin))
	assert.False(t, CanImpersonate(admin, domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleAdmin}))
	member.Active = false
	assert.False(t, CanImpersonate(admin, member))
}

func TestWriteAllowedDuringImpersonation(t *testing.T) {
	for _, tc := range []struct {
		method, route string
		want          bool
	}{
		{http.MethodGet, "/api/v1/tasks", true},
		{http.MethodHead, "/api/v1/tasks", true},
		{http.MethodOptions, "/api/v1/tasks", true},
		{http.MethodPatch, "/api/v1/tasks/42", false},
		{http.MethodDelete, "/api/admin/impersonation", true},
		{http.MethodPost, "/api/auth/logout", true},
		{http.MethodPost, "/api/admin/invitations", false},
	} {
		assert.Equal(t, tc.want, WriteAllowed(tc.method, tc.route, true), "%s %s", tc.method, tc.route)
	}
	assert.True(t, WriteAllowed(http.MethodPatch, "/api/v1/tasks/42", false))
}
