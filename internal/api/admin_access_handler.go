package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/larkaudit"

	"github.com/google/uuid"
)

const (
	defaultAuditPageSize = 50
	maxAuditPageSize     = 200
	maxAuditTimeRange    = 90 * 24 * time.Hour
)

type accessAuditReader interface {
	ListAccessAudit(context.Context, access.RequestAuditFilter) ([]access.RequestAuditEvent, error)
}

type larkAuditReader interface {
	ListLarkAPIAudit(context.Context, larkaudit.Filter) ([]larkaudit.Event, error)
}

type adminAccessHandler struct {
	tokens *access.Service
	audit  accessAuditReader
	lark   larkAuditReader
}

type adminTokenResponse struct {
	Token access.TokenMetadata `json:"token"`
	User  userResponse         `json:"user"`
}

type adminTokenListResponse struct {
	Items []adminTokenResponse `json:"items"`
}

type accessAuditPage struct {
	Items      []access.RequestAuditEvent `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

type larkAuditPage struct {
	Items      []larkaudit.Event `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type auditCursorPayload struct {
	OccurredAt time.Time `json:"occurred_at"`
	ID         uuid.UUID `json:"id"`
}

func (h *adminAccessHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	bundles, err := h.tokens.ListAllTokens(r.Context())
	if err != nil {
		slog.Error("list all API tokens failed", "request_id", requestID(r), "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list API tokens"})
		return
	}
	items := make([]adminTokenResponse, len(bundles))
	for i, bundle := range bundles {
		items[i] = adminTokenResponse{
			Token: bundle.Token.Metadata(),
			User:  userResponseFromDomain(bundle.User),
		}
	}
	WriteJSON(w, http.StatusOK, adminTokenListResponse{Items: items})
}

func (h *adminAccessHandler) revokeToken(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	tokenID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "API token not found"})
		return
	}
	if err := h.tokens.RevokeAsAdmin(r.Context(), tokenID, current.Actor.ID); err != nil {
		if errors.Is(err, access.ErrTokenNotFound) {
			WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "API token not found"})
			return
		}
		slog.Error("administrator API token revocation failed",
			"request_id", requestID(r), "actor_user_id", current.Actor.ID,
			"token_id", tokenID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to revoke API token"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminAccessHandler) activity(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	filter, err := parseAccessAuditFilter(r)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	writeAccessAuditPage(w, r, h.audit, filter)
}

func (h *adminAccessHandler) larkActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	filter, err := parseLarkAuditFilter(r)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if h.lark == nil {
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "Lark API activity unavailable"})
		return
	}
	events, err := h.lark.ListLarkAPIAudit(r.Context(), filter)
	if err != nil {
		slog.Error("list Lark API audit failed", "request_id", requestID(r), "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list Lark API activity"})
		return
	}
	pageSize := filter.Limit - 1
	response := larkAuditPage{Items: events}
	if len(events) > pageSize {
		response.Items = events[:pageSize]
		last := response.Items[len(response.Items)-1]
		response.NextCursor = encodeAuditCursor(access.RequestAuditCursor{
			OccurredAt: last.OccurredAt, ID: last.ID,
		})
	}
	if response.Items == nil {
		response.Items = []larkaudit.Event{}
	}
	WriteJSON(w, http.StatusOK, response)
}

func requireAdministrator(w http.ResponseWriter, r *http.Request) (identity.RequestIdentity, bool) {
	current, ok := identity.FromContext(r.Context())
	if !ok || !current.Actor.Active ||
		current.Actor.PlatformRole != domain.PlatformRoleAdmin ||
		current.IsImpersonating() {
		WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "administrator access required"})
		return identity.RequestIdentity{}, false
	}
	return current, true
}

func parseAccessAuditFilter(r *http.Request) (access.RequestAuditFilter, error) {
	query := r.URL.Query()
	pageSize := defaultAuditPageSize
	if raw := query.Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxAuditPageSize {
			return access.RequestAuditFilter{}, fmt.Errorf("page_size must be between 1 and %d", maxAuditPageSize)
		}
		pageSize = value
	}
	filter := access.RequestAuditFilter{
		Method:        strings.ToUpper(query.Get("method")),
		RoutePattern:  query.Get("route"),
		RequestID:     query.Get("request_id"),
		ImportantOnly: query.Get("important_only") == "true",
		Limit:         pageSize + 1,
	}
	if raw := query.Get("user_id"); raw != "" {
		value, err := uuid.Parse(raw)
		if err != nil {
			return access.RequestAuditFilter{}, errors.New("user_id must be a UUID")
		}
		filter.UserID = &value
	}
	if raw := query.Get("token_id"); raw != "" {
		value, err := uuid.Parse(raw)
		if err != nil {
			return access.RequestAuditFilter{}, errors.New("token_id must be a UUID")
		}
		filter.TokenID = &value
	}
	if raw := query.Get("status"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 100 || value > 599 {
			return access.RequestAuditFilter{}, errors.New("status must be an HTTP status code")
		}
		filter.StatusCode = &value
	}
	for name, target := range map[string]**time.Time{"from": &filter.From, "to": &filter.To} {
		if raw := query.Get(name); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return access.RequestAuditFilter{}, fmt.Errorf("%s must be an RFC3339 timestamp", name)
			}
			value = value.UTC()
			*target = &value
		}
	}
	if filter.From != nil && filter.To != nil {
		if filter.To.Before(*filter.From) || filter.To.Sub(*filter.From) > maxAuditTimeRange {
			return access.RequestAuditFilter{}, errors.New("audit time range must be ordered and no longer than 90 days")
		}
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeAuditCursor(raw)
		if err != nil {
			return access.RequestAuditFilter{}, errors.New("cursor is invalid")
		}
		filter.Before = &cursor
	}
	return filter, nil
}

func parseLarkAuditFilter(r *http.Request) (larkaudit.Filter, error) {
	query := r.URL.Query()
	pageSize := defaultAuditPageSize
	if raw := query.Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxAuditPageSize {
			return larkaudit.Filter{}, fmt.Errorf("page_size must be between 1 and %d", maxAuditPageSize)
		}
		pageSize = value
	}
	filter := larkaudit.Filter{
		Operation: query.Get("operation"), Category: query.Get("category"),
		Outcome:           larkaudit.Outcome(query.Get("outcome")),
		ProviderRequestID: query.Get("provider_request_id"),
		RequestID:         query.Get("request_id"), Limit: pageSize + 1,
	}
	if filter.Outcome != "" {
		switch filter.Outcome {
		case larkaudit.OutcomeSucceeded, larkaudit.OutcomeRejected,
			larkaudit.OutcomeRateLimited, larkaudit.OutcomeUnavailable,
			larkaudit.OutcomeCancelled, larkaudit.OutcomeContractError:
		default:
			return larkaudit.Filter{}, errors.New("outcome is invalid")
		}
	}
	if raw := query.Get("status"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 100 || value > 599 {
			return larkaudit.Filter{}, errors.New("status must be an HTTP status code")
		}
		filter.HTTPStatus = &value
	}
	for name, target := range map[string]**uuid.UUID{
		"actor_user_id": &filter.ActorUserID,
		"agent_run_id":  &filter.AgentRunID,
		"event_id":      &filter.ApplicationEventID,
	} {
		if raw := query.Get(name); raw != "" {
			value, err := uuid.Parse(raw)
			if err != nil {
				return larkaudit.Filter{}, fmt.Errorf("%s must be a UUID", name)
			}
			*target = &value
		}
	}
	for name, target := range map[string]**time.Time{"from": &filter.From, "to": &filter.To} {
		if raw := query.Get(name); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return larkaudit.Filter{}, fmt.Errorf("%s must be an RFC3339 timestamp", name)
			}
			value = value.UTC()
			*target = &value
		}
	}
	if filter.From != nil && filter.To != nil &&
		(filter.To.Before(*filter.From) || filter.To.Sub(*filter.From) > maxAuditTimeRange) {
		return larkaudit.Filter{}, errors.New("audit time range must be ordered and no longer than 90 days")
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeAuditCursor(raw)
		if err != nil {
			return larkaudit.Filter{}, errors.New("cursor is invalid")
		}
		filter.Before = &larkaudit.Cursor{OccurredAt: cursor.OccurredAt, ID: cursor.ID}
	}
	return filter, nil
}

func writeAccessAuditPage(
	w http.ResponseWriter,
	r *http.Request,
	reader accessAuditReader,
	filter access.RequestAuditFilter,
) {
	if reader == nil {
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "API activity unavailable"})
		return
	}
	events, err := reader.ListAccessAudit(r.Context(), filter)
	if err != nil {
		slog.Error("list API access audit failed", "request_id", requestID(r), "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list API activity"})
		return
	}
	pageSize := filter.Limit - 1
	response := accessAuditPage{Items: events}
	if len(events) > pageSize {
		response.Items = events[:pageSize]
		last := response.Items[len(response.Items)-1]
		response.NextCursor = encodeAuditCursor(access.RequestAuditCursor{
			OccurredAt: last.OccurredAt, ID: last.ID,
		})
	}
	if response.Items == nil {
		response.Items = []access.RequestAuditEvent{}
	}
	WriteJSON(w, http.StatusOK, response)
}

func encodeAuditCursor(cursor access.RequestAuditCursor) string {
	body, _ := json.Marshal(auditCursorPayload{OccurredAt: cursor.OccurredAt, ID: cursor.ID})
	return base64.RawURLEncoding.EncodeToString(body)
}

func decodeAuditCursor(raw string) (access.RequestAuditCursor, error) {
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return access.RequestAuditCursor{}, err
	}
	var payload auditCursorPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return access.RequestAuditCursor{}, err
	}
	if payload.OccurredAt.IsZero() || payload.ID == uuid.Nil {
		return access.RequestAuditCursor{}, errors.New("cursor fields are missing")
	}
	return access.RequestAuditCursor{OccurredAt: payload.OccurredAt, ID: payload.ID}, nil
}
