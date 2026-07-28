package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRateLimitAllowsBurstThenRefillsAtTwoPerSecond(t *testing.T) {
	limiter := newTokenBucketLimiter()
	tokenID := uuid.New()
	now := time.Date(2026, 7, 28, 17, 0, 0, 0, time.UTC)

	for request := 1; request <= 30; request++ {
		decision := limiter.Allow(tokenID, now)
		require.True(t, decision.Allowed, "burst request %d", request)
		require.Equal(t, 120, decision.Limit)
		require.Equal(t, 30-request, decision.Remaining)
	}
	denied := limiter.Allow(tokenID, now)
	require.False(t, denied.Allowed)
	require.Zero(t, denied.Remaining)
	require.Equal(t, 500*time.Millisecond, denied.ResetAfter)

	refilled := limiter.Allow(tokenID, now.Add(500*time.Millisecond))
	require.True(t, refilled.Allowed)
	require.Zero(t, refilled.Remaining)
}

func TestRateLimitIsIsolatedPerTokenAndPrunesInactiveBuckets(t *testing.T) {
	limiter := newTokenBucketLimiter()
	now := time.Date(2026, 7, 28, 17, 0, 0, 0, time.UTC)
	first, second := uuid.New(), uuid.New()

	for range 30 {
		require.True(t, limiter.Allow(first, now).Allowed)
	}
	require.False(t, limiter.Allow(first, now).Allowed)
	require.True(t, limiter.Allow(second, now).Allowed)
	require.Len(t, limiter.buckets, 2)

	limiter.prune(now.Add(rateLimitBucketIdleTTL + time.Second))
	require.Empty(t, limiter.buckets)
}

func TestRateLimitMiddlewareEmitsHeadersAndProblem(t *testing.T) {
	tokenID := uuid.New()
	now := time.Date(2026, 7, 28, 17, 0, 0, 0, time.UTC)
	authenticator := &fakeBearerAuthenticator{principal: access.Principal{
		User:   domain.User{ID: uuid.New(), Active: true},
		Method: access.AuthenticationMethodAPIToken, TokenID: &tokenID,
		Scopes: []access.Scope{access.ScopeWorkRead},
	}}
	limiter := newTokenBucketLimiter()
	handler := RequestIDMiddleware(bearerAuthentication{
		tokens: authenticator, limiter: limiter, now: func() time.Time { return now },
	}.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), http.NotFoundHandler()))

	var response *httptest.ResponseRecorder
	for range 31 {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		request.Header.Set("Authorization", "Bearer token")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
	}

	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Equal(t, "120", response.Header().Get("RateLimit-Limit"))
	require.Equal(t, "0", response.Header().Get("RateLimit-Remaining"))
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	_, err := strconv.Atoi(response.Header().Get("RateLimit-Reset"))
	require.NoError(t, err)
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "RATE_LIMITED", problem.Code)
}
