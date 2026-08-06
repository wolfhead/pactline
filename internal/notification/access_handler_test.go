package notification

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recipientResolverStub struct {
	recipient identity.ExternalIdentity
}

func (stub recipientResolverStub) GetExternalIdentityForUser(
	context.Context,
	uuid.UUID,
) (identity.ExternalIdentity, error) {
	return stub.recipient, nil
}

type cardSenderStub struct {
	recipient      identity.PrincipalKey
	card           Card
	idempotencyKey string
}

func (stub *cardSenderStub) SendCard(
	_ context.Context,
	recipient identity.PrincipalKey,
	card Card,
	idempotencyKey string,
) (identity.DeliveryReceipt, error) {
	stub.recipient = recipient
	stub.card = card
	stub.idempotencyKey = idempotencyKey
	return identity.DeliveryReceipt{ProviderReference: "message-1"}, nil
}

func TestAccessHandlerRendersRequestedAndApprovedCards(t *testing.T) {
	baseURL, err := url.Parse("https://pactline.example.test")
	require.NoError(t, err)
	recipient := identity.PrincipalKey{
		Provider: "lark", TenantID: "tenant", SubjectID: "ou_recipient",
	}
	now := time.Date(2026, 8, 6, 12, 30, 0, 0, time.UTC)
	requesterID := uuid.New()
	email := "applicant@example.test"

	tests := []struct {
		name       string
		eventType  string
		payload    any
		wantTitle  string
		wantAction string
		wantURL    string
		wantLine   string
	}{
		{
			name: "requested", eventType: events.AccessRequested,
			payload: events.AccessRequestedPayload{
				RequesterID: requesterID, RequesterName: "Applicant",
				RequesterEmail: &email, RequestedAt: now,
			},
			wantTitle: "新的 Pactline 访问申请", wantAction: "前往 Pactline 审批",
			wantURL: "https://pactline.example.test/admin/users", wantLine: "申请人：Applicant",
		},
		{
			name: "approved", eventType: events.AccessApproved,
			payload: events.AccessApprovedPayload{
				UserID: requesterID, UserName: "Applicant", ApprovedByID: uuid.New(),
				ApprovedByName: "Administrator", ApprovedAt: now,
			},
			wantTitle: "Pactline 访问申请已通过", wantAction: "进入 Pactline",
			wantURL: "https://pactline.example.test/", wantLine: "Administrator 已通过你的访问申请。",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recipientID := uuid.New()
			if test.eventType == events.AccessApproved {
				recipientID = requesterID
			}
			event, eventErr := events.New(events.NewEvent{
				AggregateType: "access_request", AggregateID: requesterID,
				Type: test.eventType, RecipientID: recipientID, Payload: test.payload,
				DedupeKey: test.eventType + ":" + requesterID.String(), CreatedAt: now,
			})
			require.NoError(t, eventErr)
			sender := &cardSenderStub{}
			handler := AccessHandler{
				Recipients: recipientResolverStub{recipient: identity.ExternalIdentity{Key: recipient}},
				Sender:     sender, AppBaseURL: baseURL,
			}

			require.NoError(t, handler.Handle(context.Background(), event))
			require.Equal(t, recipient, sender.recipient)
			require.Equal(t, event.ID.String(), sender.idempotencyKey)
			require.Equal(t, test.wantTitle, sender.card.Title)
			require.Equal(t, test.wantAction, sender.card.ActionLabel)
			require.Equal(t, test.wantURL, sender.card.ActionURL)
			require.Contains(t, sender.card.Lines, test.wantLine)
			require.NoError(t, sender.card.Validate())
		})
	}
}
