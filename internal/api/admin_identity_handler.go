package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/identity"

	"github.com/google/uuid"
)

type adminIdentityHandler struct {
	service *identity.Service
}

type directoryPrincipalResponse struct {
	SubjectID string  `json:"subject_id"`
	Name      string  `json:"name"`
	Email     *string `json:"email"`
	AvatarURL *string `json:"avatar_url"`
}

type invitationResponse struct {
	ID              uuid.UUID                 `json:"id"`
	TargetSubjectID string                    `json:"target_subject_id"`
	TargetSnapshot  json.RawMessage           `json:"target_snapshot"`
	Status          identity.InvitationStatus `json:"status"`
	ExpiresAt       time.Time                 `json:"expires_at"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
	Delivery        *deliveryResponse         `json:"delivery,omitempty"`
}

type deliveryResponse struct {
	Channel       identity.DeliveryChannel        `json:"channel"`
	Status        identity.DeliveryStatus         `json:"status"`
	ErrorCategory *identity.ProviderErrorCategory `json:"error_category"`
	AttemptedAt   time.Time                       `json:"attempted_at"`
}

func (h *adminIdentityHandler) searchDirectory(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	principals, err := h.service.SearchDirectory(r.Context(), current.Actor, r.URL.Query().Get("q"))
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "query must contain at least 2 characters"})
			return
		}
		slog.Warn("Lark directory search failed", "actor_user_id", current.Actor.ID, "error_category", "directory")
		WriteJSON(w, http.StatusBadGateway, ErrorBody{Error: "directory search unavailable"})
		return
	}
	response := make([]directoryPrincipalResponse, len(principals))
	for index, principal := range principals {
		response[index] = directoryPrincipalResponse{
			SubjectID: principal.Key.SubjectID, Name: principal.Name,
			Email: principal.Email, AvatarURL: principal.AvatarURL,
		}
	}
	WriteJSON(w, http.StatusOK, response)
}

func (h *adminIdentityHandler) listInvitations(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	invitations, err := h.service.ListInvitations(r.Context(), current.Actor)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list invitations"})
		return
	}
	response := make([]invitationResponse, len(invitations))
	for index, result := range invitations {
		var delivery *identity.InvitationDelivery
		if result.Delivery.ID != uuid.Nil {
			delivery = &result.Delivery
		}
		response[index] = invitationView(result.Invitation, delivery)
	}
	WriteJSON(w, http.StatusOK, response)
}

func (h *adminIdentityHandler) createInvitation(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		SubjectID string `json:"subject_id"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	result, err := h.service.CreateInvitation(r.Context(), current.Actor, request.SubjectID, requestID(r))
	if err != nil {
		h.writeInvitationError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, invitationView(result.Invitation, &result.Delivery))
}

func (h *adminIdentityHandler) resendInvitation(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "invitation not found"})
		return
	}
	result, err := h.service.ResendInvitation(r.Context(), current.Actor, id)
	if err != nil {
		h.writeInvitationError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, invitationView(result.Invitation, &result.Delivery))
}

func (h *adminIdentityHandler) rotateInvitationLink(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "invitation not found"})
		return
	}
	link, err := h.service.RotateInvitationLink(r.Context(), current.Actor, id)
	if err != nil {
		h.writeInvitationError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"url": link})
}

func (h *adminIdentityHandler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "invitation not found"})
		return
	}
	if err := h.service.RevokeInvitation(r.Context(), current.Actor, id, requestID(r)); err != nil {
		h.writeInvitationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminIdentityHandler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	start, err := h.service.AcceptInvitationToken(r.Context(), request.Token)
	if err != nil {
		WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "invitation is invalid or expired"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"authorization_url": start.URL})
}

func (h *adminIdentityHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	users, err := h.service.ListAdminUsers(r.Context(), current.Actor)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list users"})
		return
	}
	WriteJSON(w, http.StatusOK, userResponses(users))
}

func (h *adminIdentityHandler) updateUser(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "user not found"})
		return
	}
	var request struct {
		Active *bool `json:"active"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	if request.Active == nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "active is required"})
		return
	}
	if err := h.service.SetUserActive(r.Context(), current.Actor, id, *request.Active); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "user not found"})
		case errors.Is(err, domain.ErrForbidden):
			WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "administrator cannot be deactivated"})
		default:
			WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to update user"})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminIdentityHandler) startImpersonation(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	if request.UserID == uuid.Nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "user_id is required"})
		return
	}
	if err := h.service.StartImpersonation(r.Context(), current, request.UserID, requestID(r)); err != nil {
		switch {
		case errors.Is(err, identity.ErrImpersonationActive):
			WriteJSON(w, http.StatusConflict, ErrorBody{Error: "impersonation already active"})
		case errors.Is(err, identity.ErrImpersonationDenied):
			WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "impersonation denied"})
		default:
			slog.Error("start impersonation failed", "actor_user_id", current.Actor.ID, "error_category", "persistence")
			WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to start impersonation"})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminIdentityHandler) endImpersonation(w http.ResponseWriter, r *http.Request) {
	current, ok := identity.FromContext(r.Context())
	if !ok || !current.IsImpersonating() || current.Actor.PlatformRole != domain.PlatformRoleAdmin {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "active impersonation not found"})
		return
	}
	if err := h.service.EndImpersonation(r.Context(), current, requestID(r)); err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "active impersonation not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminIdentityHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (identity.RequestIdentity, bool) {
	current, ok := identity.FromContext(r.Context())
	if !ok || !current.Actor.Active || current.Actor.PlatformRole != domain.PlatformRoleAdmin ||
		current.IsImpersonating() {
		WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "administrator access required"})
		return identity.RequestIdentity{}, false
	}
	return current, true
}

func (h *adminIdentityHandler) writeInvitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvitationConflict), errors.Is(err, domain.ErrConflict):
		WriteJSON(w, http.StatusConflict, ErrorBody{Error: "an account or pending invitation already exists"})
	case errors.Is(err, identity.ErrInvitationInvalid), errors.Is(err, domain.ErrNotFound):
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "invitation not found"})
	case errors.Is(err, identity.ErrAdminRequired):
		WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "administrator access required"})
	case errors.Is(err, domain.ErrInvalidInput):
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid invitation request"})
	default:
		slog.Error("invitation operation failed", "error_category", "internal")
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "invitation operation failed"})
	}
}

func invitationView(invitation identity.Invitation, delivery *identity.InvitationDelivery) invitationResponse {
	response := invitationResponse{
		ID: invitation.ID, TargetSubjectID: invitation.Target.SubjectID,
		TargetSnapshot: invitation.TargetSnapshot, Status: invitation.Status,
		ExpiresAt: invitation.ExpiresAt, CreatedAt: invitation.CreatedAt, UpdatedAt: invitation.UpdatedAt,
	}
	if delivery != nil {
		response.Delivery = &deliveryResponse{
			Channel: delivery.Channel, Status: delivery.Status,
			ErrorCategory: delivery.ErrorCategory, AttemptedAt: delivery.AttemptedAt,
		}
	}
	return response
}

func decodeStrictBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		slog.Warn("decode strict request body", "path", r.URL.Path, "error_category", "invalid_json")
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid JSON body"})
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid JSON body"})
		return false
	}
	return true
}
