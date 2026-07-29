package v1

import (
	"context"
	"errors"

	"bountyboard/internal/access"
	generated "bountyboard/internal/api/v1generated"
	"bountyboard/internal/identity"

	"github.com/ogen-go/ogen/ogenerrors"
)

var (
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrInsufficientScope      = errors.New("insufficient scope")
	ErrInvalidRequest         = errors.New("invalid request")
)

type Security struct{}

func (Security) HandleBearerAuth(
	ctx context.Context,
	operation generated.OperationName,
	_ generated.BearerAuth,
) (context.Context, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return ctx, ErrAuthenticationRequired
	}
	if current.AuthenticationMethod != access.AuthenticationMethodAPIToken {
		return ctx, ogenerrors.ErrSkipServerSecurity
	}
	principal := access.Principal{Scopes: current.Scopes}
	if !principal.HasScope(requiredScope(operation)) {
		return ctx, ErrInsufficientScope
	}
	return ctx, nil
}

func (Security) HandleSessionCookie(
	ctx context.Context,
	_ generated.OperationName,
	_ generated.SessionCookie,
) (context.Context, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return ctx, ErrAuthenticationRequired
	}
	if current.AuthenticationMethod != access.AuthenticationMethodSession {
		return ctx, ogenerrors.ErrSkipServerSecurity
	}
	return ctx, nil
}

func requiredScope(operation generated.OperationName) access.Scope {
	switch operation {
	case generated.GetCurrentPrincipalOperation,
		generated.GetProjectOperation,
		generated.GetTaskOperation,
		generated.ListLabelsOperation,
		generated.ListMilestoneCriteriaOperation,
		generated.ListProjectCriteriaOperation,
		generated.ListProjectsOperation,
		generated.ListTaskActivityOperation,
		generated.ListTaskCommentsOperation,
		generated.ListTaskCriteriaOperation,
		generated.ListTasksOperation,
		generated.ListUsersOperation:
		return access.ScopeWorkRead
	default:
		return access.ScopeWorkWrite
	}
}
