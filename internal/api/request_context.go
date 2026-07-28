package api

import (
	"context"
	"net/http"
)

type requestIDContextKey struct{}

func withRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, value)
}

func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDContextKey{}).(string)
	return value
}
