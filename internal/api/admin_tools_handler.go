package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/notification"

	"github.com/google/uuid"
)

type adminNotificationTester interface {
	ListRecipients(context.Context, domain.User) ([]domain.User, error)
	RequestDMTest(context.Context, domain.User, uuid.UUID) (events.Event, error)
}

type adminToolsHandler struct {
	notifications adminNotificationTester
}

func (handler *adminToolsHandler) listNotificationRecipients(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	recipients, err := handler.notifications.ListRecipients(r.Context(), current.Actor)
	if err != nil {
		slog.Error("list notification test recipients failed",
			"actor_user_id", current.Actor.ID, "request_id", requestID(r), "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list notification recipients"})
		return
	}
	WriteJSON(w, http.StatusOK, userResponses(recipients))
}

func (handler *adminToolsHandler) requestDMTest(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	var request struct {
		RecipientID uuid.UUID `json:"recipient_id"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	event, err := handler.notifications.RequestDMTest(r.Context(), current.Actor, request.RecipientID)
	if err != nil {
		if errors.Is(err, notification.ErrTestRecipientUnavailable) {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "recipient is unavailable for Lark DM tests"})
			return
		}
		slog.Error("request notification test failed",
			"actor_user_id", current.Actor.ID, "recipient_user_id", request.RecipientID,
			"request_id", requestID(r), "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to request notification test"})
		return
	}
	slog.Info("notification test event requested",
		"actor_user_id", current.Actor.ID, "recipient_user_id", request.RecipientID,
		"event_id", event.ID, "request_id", requestID(r))
	WriteJSON(w, http.StatusAccepted, struct {
		EventID uuid.UUID `json:"event_id"`
		Status  string    `json:"status"`
	}{EventID: event.ID, Status: "queued"})
}
