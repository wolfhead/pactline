package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type adminNotificationTesterStub struct {
	recipients []domain.User
	event      events.Event
	gotActor   domain.User
	gotUserID  uuid.UUID
}

func (stub *adminNotificationTesterStub) ListRecipients(
	_ context.Context,
	actor domain.User,
) ([]domain.User, error) {
	stub.gotActor = actor
	return stub.recipients, nil
}

func (stub *adminNotificationTesterStub) RequestDMTest(
	_ context.Context,
	actor domain.User,
	recipientID uuid.UUID,
) (events.Event, error) {
	stub.gotActor = actor
	stub.gotUserID = recipientID
	return stub.event, nil
}

func TestAdminToolsListsRecipientsAndQueuesDMTest(t *testing.T) {
	admin := domain.User{
		ID: uuid.New(), Name: "Administrator", PlatformRole: domain.PlatformRoleAdmin,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
	recipient := domain.User{
		ID: uuid.New(), Name: "Member", PlatformRole: domain.PlatformRoleMember,
		AccessStatus: domain.AccessStatusApproved, Active: true,
		CreatedAt: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC),
	}
	eventID := uuid.New()
	stub := &adminNotificationTesterStub{
		recipients: []domain.User{recipient},
		event:      events.Event{ID: eventID},
	}
	handler := &adminToolsHandler{notifications: stub}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/admin/tools/notifications/recipients", nil)
	listRequest = listRequest.WithContext(identity.WithRequestIdentity(listRequest.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	listResponse := httptest.NewRecorder()
	handler.listNotificationRecipients(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code)
	require.Contains(t, listResponse.Body.String(), recipient.ID.String())

	request := httptest.NewRequest(http.MethodPost, "/api/admin/tools/notifications/test",
		bytes.NewBufferString(`{"recipient_id":"`+recipient.ID.String()+`"}`))
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response := httptest.NewRecorder()
	handler.requestDMTest(response, request)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.JSONEq(t, `{"event_id":"`+eventID.String()+`","status":"queued"}`, response.Body.String())
	require.Equal(t, admin.ID, stub.gotActor.ID)
	require.Equal(t, recipient.ID, stub.gotUserID)
}

func TestAdminToolsRejectsMember(t *testing.T) {
	member := domain.User{
		ID: uuid.New(), Name: "Member", PlatformRole: domain.PlatformRoleMember,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/tools/notifications/recipients", nil)
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: member, Subject: member}))
	response := httptest.NewRecorder()

	(&adminToolsHandler{notifications: &adminNotificationTesterStub{}}).
		listNotificationRecipients(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
}
