package api

import (
	"context"
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/identity"
)

type requestIDContextKey struct{}

func withRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, value)
}

func requestID(r *http.Request) string {
	return RequestIDFromContext(r.Context())
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func operationActor(r *http.Request) (domain.OperationActor, bool) {
	current, ok := identity.FromContext(r.Context())
	if !ok {
		return domain.OperationActor{}, false
	}
	actor := domain.OperationActor{
		UserID:     current.Actor.ID,
		AuthMethod: current.AuthenticationMethod,
		TokenID:    current.APITokenID,
		TokenName:  current.APITokenName,
		RequestID:  requestID(r),
	}
	return actor, actor.Validate() == nil
}
