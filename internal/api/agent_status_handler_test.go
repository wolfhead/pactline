package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type agentStatusStub struct {
	status channel.ConnectionStatus
}

func (s agentStatusStub) Snapshot() channel.ConnectionStatus {
	return s.status
}

func TestAgentStatusRequiresAdministratorAndReturnsSafeLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	handler := &agentStatusHandler{status: agentStatusStub{
		status: channel.ConnectionStatus{
			Enabled: true, State: channel.ConnectionReconnecting,
			LastTransitionAt: now, ReconnectCount: 2,
			LastErrorCategory: "provider_connection",
		},
	}}

	for name, role := range map[string]domain.PlatformRole{
		"member": domain.PlatformRoleMember,
		"admin":  domain.PlatformRoleAdmin,
	} {
		t.Run(name, func(t *testing.T) {
			userID := uuid.New()
			user := domain.User{
				ID: userID, Name: "User", Active: true,
				PlatformRole: role,
			}
			request := httptest.NewRequest(http.MethodGet, "/api/admin/agent/status", nil)
			request = request.WithContext(identity.WithRequestIdentity(
				request.Context(),
				identity.RequestIdentity{
					Actor:   user,
					Subject: user,
				},
			))
			response := httptest.NewRecorder()

			handler.get(response, request)

			if role == domain.PlatformRoleMember {
				require.Equal(t, http.StatusForbidden, response.Code)
				return
			}
			require.Equal(t, http.StatusOK, response.Code)
			require.JSONEq(t, `{
				"enabled":true,
				"state":"reconnecting",
				"last_transition_at":"2026-07-30T12:00:00Z",
				"reconnect_count":2,
				"last_error_category":"provider_connection"
			}`, response.Body.String())
		})
	}
}
