package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

var ErrTestRecipientUnavailable = errors.New("notification test recipient is unavailable")

type TestUserRepository interface {
	GetByID(context.Context, uuid.UUID) (domain.User, error)
}

type TestRecipientRepository interface {
	RecipientResolver
	ListExternalIdentityUsers(context.Context, string) ([]domain.User, error)
}

type TestEventWriter interface {
	Enqueue(context.Context, events.Event) error
}

type TestService struct {
	Users      TestUserRepository
	Recipients TestRecipientRepository
	Events     TestEventWriter
	Now        func() time.Time
}

func (service *TestService) ListRecipients(
	ctx context.Context,
	actor domain.User,
) ([]domain.User, error) {
	if err := service.validate(actor); err != nil {
		return nil, err
	}
	return service.Recipients.ListExternalIdentityUsers(ctx, "lark")
}

func (service *TestService) RequestDMTest(
	ctx context.Context,
	actor domain.User,
	recipientID uuid.UUID,
) (events.Event, error) {
	if err := service.validate(actor); err != nil {
		return events.Event{}, err
	}
	if recipientID == uuid.Nil {
		return events.Event{}, ErrTestRecipientUnavailable
	}
	recipient, err := service.Users.GetByID(ctx, recipientID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return events.Event{}, ErrTestRecipientUnavailable
		}
		return events.Event{}, fmt.Errorf("load notification test recipient: %w", err)
	}
	if !recipient.CanUseApplication() {
		return events.Event{}, ErrTestRecipientUnavailable
	}
	external, err := service.Recipients.GetExternalIdentityForUser(ctx, recipientID)
	if err != nil {
		if errors.Is(err, identity.ErrCredentialNotFound) || errors.Is(err, domain.ErrNotFound) {
			return events.Event{}, ErrTestRecipientUnavailable
		}
		return events.Event{}, fmt.Errorf("load notification test identity: %w", err)
	}
	if external.Key.Provider != "lark" || strings.TrimSpace(external.Key.SubjectID) == "" {
		return events.Event{}, ErrTestRecipientUnavailable
	}

	now := service.Now().UTC()
	eventID := uuid.New()
	event, err := events.New(events.NewEvent{
		ID: eventID, AggregateType: "notification_test", AggregateID: eventID,
		Type: events.NotificationTest, RecipientID: recipientID,
		Payload: events.NotificationTestPayload{
			TriggeredByID: actor.ID, TriggeredByName: actor.Name, TriggeredAt: now,
		},
		DedupeKey: events.NotificationTest + ":" + eventID.String(), CreatedAt: now,
	})
	if err != nil {
		return events.Event{}, fmt.Errorf("build notification test event: %w", err)
	}
	if err := service.Events.Enqueue(ctx, event); err != nil {
		return events.Event{}, fmt.Errorf("enqueue notification test event: %w", err)
	}
	return event, nil
}

func (service *TestService) validate(actor domain.User) error {
	if service == nil || service.Users == nil || service.Recipients == nil ||
		service.Events == nil || service.Now == nil {
		return fmt.Errorf("notification test service is not configured")
	}
	if !actor.CanUseApplication() || actor.PlatformRole != domain.PlatformRoleAdmin {
		return identity.ErrAdminRequired
	}
	return nil
}
