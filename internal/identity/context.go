package identity

import (
	"context"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

type RequestIdentity struct {
	SessionID            uuid.UUID
	Actor                domain.User
	Subject              domain.User
	Impersonation        *Impersonation
	AuthenticationMethod access.AuthenticationMethod
	APITokenID           *uuid.UUID
	APITokenName         string
	Scopes               []access.Scope
}

func (i RequestIdentity) IsAdmin() bool {
	return i.Actor.PlatformRole == domain.PlatformRoleAdmin
}

func (i RequestIdentity) IsImpersonating() bool {
	return i.Impersonation != nil
}

type requestIdentityKey struct{}

func WithRequestIdentity(ctx context.Context, requestIdentity RequestIdentity) context.Context {
	return context.WithValue(ctx, requestIdentityKey{}, requestIdentity)
}

func IdentityFromContext(ctx context.Context) (RequestIdentity, bool) {
	requestIdentity, ok := ctx.Value(requestIdentityKey{}).(RequestIdentity)
	return requestIdentity, ok
}

func FromContext(ctx context.Context) (RequestIdentity, bool) {
	return IdentityFromContext(ctx)
}

func SubjectUserID(ctx context.Context) (uuid.UUID, bool) {
	requestIdentity, ok := IdentityFromContext(ctx)
	return requestIdentity.Subject.ID, ok
}

func ActorUserID(ctx context.Context) (uuid.UUID, bool) {
	requestIdentity, ok := IdentityFromContext(ctx)
	return requestIdentity.Actor.ID, ok
}
