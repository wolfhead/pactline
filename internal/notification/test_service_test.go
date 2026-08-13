package notification

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type testUserRepositoryStub struct {
	user domain.User
	err  error
}

func (stub testUserRepositoryStub) GetByID(context.Context, uuid.UUID) (domain.User, error) {
	return stub.user, stub.err
}

type testRecipientRepositoryStub struct {
	users    []domain.User
	external identity.ExternalIdentity
	err      error
}

func (stub testRecipientRepositoryStub) ListExternalIdentityUsers(
	context.Context,
	string,
) ([]domain.User, error) {
	return stub.users, stub.err
}

func (stub testRecipientRepositoryStub) GetExternalIdentityForUser(
	context.Context,
	uuid.UUID,
) (identity.ExternalIdentity, error) {
	return stub.external, stub.err
}

type testEventWriterStub struct {
	event events.Event
}

func (stub *testEventWriterStub) Enqueue(_ context.Context, event events.Event) error {
	stub.event = event
	return nil
}

func TestTestServiceQueuesTypedEventForEligibleLarkRecipient(t *testing.T) {
	now := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)
	admin := domain.User{
		ID: uuid.New(), Name: "Administrator", PlatformRole: domain.PlatformRoleAdmin,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
	recipient := domain.User{
		ID: uuid.New(), Name: "Member", PlatformRole: domain.PlatformRoleMember,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
	writer := &testEventWriterStub{}
	service := &TestService{
		Users: testUserRepositoryStub{user: recipient},
		Recipients: testRecipientRepositoryStub{
			users: []domain.User{recipient},
			external: identity.ExternalIdentity{Key: identity.PrincipalKey{
				Provider: "lark", TenantID: "tenant", SubjectID: "ou_member",
			}},
		},
		Events: writer, Now: func() time.Time { return now },
	}

	recipients, err := service.ListRecipients(context.Background(), admin)
	require.NoError(t, err)
	require.Equal(t, []domain.User{recipient}, recipients)
	event, err := service.RequestDMTest(context.Background(), admin, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, event, writer.event)
	require.Equal(t, events.NotificationTest, event.Type)
	require.Equal(t, recipient.ID, event.RecipientID)
	require.Equal(t, event.ID, event.AggregateID)
	payload, err := events.DecodePayload[events.NotificationTestPayload](event)
	require.NoError(t, err)
	require.Equal(t, admin.ID, payload.TriggeredByID)
	require.Equal(t, now, payload.TriggeredAt)
}

func TestTestServiceRejectsUnavailableRecipient(t *testing.T) {
	admin := domain.User{
		ID: uuid.New(), Name: "Administrator", PlatformRole: domain.PlatformRoleAdmin,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
	inactive := domain.User{
		ID: uuid.New(), Name: "Inactive", PlatformRole: domain.PlatformRoleMember,
		AccessStatus: domain.AccessStatusApproved, Active: false,
	}
	service := &TestService{
		Users: testUserRepositoryStub{user: inactive},
		Recipients: testRecipientRepositoryStub{external: identity.ExternalIdentity{Key: identity.PrincipalKey{
			Provider: "lark", TenantID: "tenant", SubjectID: "ou_member",
		}}},
		Events: &testEventWriterStub{}, Now: time.Now,
	}

	_, err := service.RequestDMTest(context.Background(), admin, inactive.ID)
	require.ErrorIs(t, err, ErrTestRecipientUnavailable)
}
