package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdminIdentityHandlerRejectsMemberAndImpersonatingAdmin(t *testing.T) {
	handler := &adminIdentityHandler{}
	member := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	admin := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleAdmin}
	impersonation := &identity.Impersonation{
		ID: uuid.New(), SessionID: uuid.New(), ActorUserID: admin.ID,
		SubjectUserID: member.ID, StartedAt: time.Now().UTC(),
	}
	for name, current := range map[string]identity.RequestIdentity{
		"member": {Actor: member, Subject: member},
		"impersonating administrator": {
			Actor: admin, Subject: member, SessionID: impersonation.SessionID,
			Impersonation: impersonation,
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/admin/invitations", nil)
			request = request.WithContext(identity.WithRequestIdentity(request.Context(), current))
			response := httptest.NewRecorder()
			handler.listInvitations(response, request)
			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}

func TestInvitationHTTPDTOAndStrictInputNeverExposeToken(t *testing.T) {
	rawToken := "raw-invitation-token"
	hash := identity.HashSecret([]byte(rawToken))
	view := invitationView(identity.Invitation{
		ID: uuid.New(), Target: identity.PrincipalKey{SubjectID: "ou_member"},
		TargetSnapshot: json.RawMessage(`{"name":"Member"}`), TokenHash: hash[:],
		Status: identity.InvitationPending, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}, nil)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), rawToken)
	require.NotContains(t, string(encoded), "token_hash")

	request := httptest.NewRequest(http.MethodPost, "/api/invitations/accept",
		bytes.NewBufferString(`{"token":"value","subject_id":"ou_member"}`))
	response := httptest.NewRecorder()
	(&adminIdentityHandler{}).acceptInvitation(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.False(t, strings.Contains(response.Body.String(), "ou_member"))
}
