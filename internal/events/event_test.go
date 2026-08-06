package events

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewBuildsValidatedEventWithTypedPayload(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	requesterID := uuid.New()
	event, err := New(NewEvent{
		AggregateType: "access_request", AggregateID: requesterID,
		Type: AccessRequested, RecipientID: uuid.New(),
		Payload: AccessRequestedPayload{
			RequesterID: requesterID, RequesterName: "Applicant", RequestedAt: now,
		},
		DedupeKey: AccessRequested + ":" + requesterID.String(), CreatedAt: now,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, event.ID)
	require.NoError(t, event.Validate())
	payload, err := DecodePayload[AccessRequestedPayload](event)
	require.NoError(t, err)
	require.Equal(t, requesterID, payload.RequesterID)

	_, err = New(NewEvent{Type: AccessRequested})
	require.Error(t, err)
	_, err = New(NewEvent{
		AggregateType: "access_request", AggregateID: requesterID,
		Type: AccessRequested, RecipientID: uuid.New(),
		Payload:   AccessRequestedPayload{RequesterID: requesterID, RequestedAt: now},
		DedupeKey: "invalid", CreatedAt: now,
	})
	require.Error(t, err)
}
