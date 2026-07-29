package v1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	baseapi "bountyboard/internal/api"
	generated "bountyboard/internal/api/v1generated"
	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/identity"
	"bountyboard/internal/store"
)

type Handler struct {
	generated.UnimplementedHandler
	Users  *store.UserStore
	Tasks  *application.TaskService
	Labels *application.LabelService
}

func (h *Handler) GetCurrentPrincipal(ctx context.Context) (generated.GetCurrentPrincipalRes, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return nil, ErrAuthenticationRequired
	}
	scopes := make([]generated.CurrentPrincipalScopesItem, len(current.Scopes))
	for i, scope := range current.Scopes {
		scopes[i] = generated.CurrentPrincipalScopesItem(scope)
	}
	response := generated.CurrentPrincipal{
		Actor:                userFromDomain(current.Actor),
		Subject:              userFromDomain(current.Subject),
		AuthenticationMethod: generated.CurrentPrincipalAuthenticationMethod(current.AuthenticationMethod),
		Scopes:               scopes,
		Impersonating:        current.IsImpersonating(),
	}
	return &generated.CurrentPrincipalHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) ListUsers(
	ctx context.Context,
	params generated.ListUsersParams,
) (generated.ListUsersRes, error) {
	users, err := h.Users.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active users: %w", err)
	}
	offset, err := decodeCursor(params.Cursor)
	if err != nil || offset > len(users) {
		return nil, ErrInvalidRequest
	}
	limit := 50
	if value, ok := params.Limit.Get(); ok {
		limit = value
	}
	end := min(len(users), offset+limit)
	items := make([]generated.User, end-offset)
	for i, user := range users[offset:end] {
		items[i] = userFromDomain(user)
	}
	response := generated.UserList{Items: items}
	if end < len(users) {
		response.NextCursor = generated.NewOptString(encodeCursor(end))
	}
	return &generated.UserListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func userFromDomain(user domain.User) generated.User {
	out := generated.User{
		ID: user.ID, Name: user.Name, Active: user.Active,
		PlatformRole: generated.UserPlatformRole(user.PlatformRole),
	}
	if user.Email != nil {
		out.Email = generated.NewOptString(*user.Email)
	}
	if user.AvatarURL != nil {
		if parsed, err := url.Parse(*user.AvatarURL); err == nil {
			out.AvatarURL = generated.NewOptURI(*parsed)
		}
	}
	return out
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor generated.OptString) (int, error) {
	value, ok := cursor.Get()
	if !ok || value == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return offset, nil
}
