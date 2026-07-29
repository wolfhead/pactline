package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type memoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[access.IdempotencyKey]memoryIdempotencyRecord
}

type memoryIdempotencyRecord struct {
	hash      string
	completed bool
	response  access.StoredResponse
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{records: map[access.IdempotencyKey]memoryIdempotencyRecord{}}
}

func (s *memoryIdempotencyStore) Claim(
	_ context.Context,
	key access.IdempotencyKey,
	hash []byte,
	_, _ time.Time,
) (access.Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[key]
	if !exists {
		s.records[key] = memoryIdempotencyRecord{hash: string(hash)}
		return access.Claim{Kind: access.ClaimAcquired}, nil
	}
	if record.hash != string(hash) {
		return access.Claim{Kind: access.ClaimReused}, nil
	}
	if !record.completed {
		return access.Claim{Kind: access.ClaimInProgress}, nil
	}
	return access.Claim{Kind: access.ClaimReplay, Response: record.response}, nil
}

func (s *memoryIdempotencyStore) Complete(
	_ context.Context,
	key access.IdempotencyKey,
	response access.StoredResponse,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[key]
	record.completed, record.response = true, response
	s.records[key] = record
	return nil
}

func (s *memoryIdempotencyStore) Release(_ context.Context, key access.IdempotencyKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
	return nil
}

func TestIdempotencyRequiresOneBoundedVisibleKey(t *testing.T) {
	for _, key := range []string{"", string(bytes.Repeat([]byte("x"), 129)), "contains space"} {
		t.Run(key, func(t *testing.T) {
			handler := idempotencyTestHandler(
				t, newMemoryIdempotencyStore(),
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			response := performIdempotentRequest(handler, "request-id", key, `{"title":"x"}`)
			require.Equal(t, http.StatusBadRequest, response.Code)
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			if key == "" {
				require.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", problem.Code)
			} else {
				require.Equal(t, "INVALID_REQUEST", problem.Code)
			}
		})
	}
}

func TestIdempotencyReplaysByteIdenticalResponseAndSelectedHeaders(t *testing.T) {
	var executions atomic.Int64
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"7"`)
		w.Header().Set("Location", "/api/v1/tasks/7")
		w.Header().Set("X-Must-Not-Replay", "secret")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7}`))
	})
	handler := idempotencyTestHandler(t, newMemoryIdempotencyStore(), downstream)

	first := performIdempotentRequest(handler, "first-request", "create-task", `{"title":"x"}`)
	replayed := performIdempotentRequest(handler, "second-request", "create-task", `{"title":"x"}`)

	require.EqualValues(t, 1, executions.Load())
	require.Equal(t, first.Code, replayed.Code)
	require.Equal(t, first.Body.Bytes(), replayed.Body.Bytes())
	require.Equal(t, `"7"`, replayed.Header().Get("ETag"))
	require.Equal(t, "/api/v1/tasks/7", replayed.Header().Get("Location"))
	require.Empty(t, replayed.Header().Get("X-Must-Not-Replay"))
	require.Equal(t, "true", replayed.Header().Get("Idempotency-Replayed"))
	require.Equal(t, "second-request", replayed.Header().Get("X-Request-ID"))
}

func TestIdempotencyRejectsDifferentBodyAndConcurrentExecution(t *testing.T) {
	store := newMemoryIdempotencyStore()
	started, release := make(chan struct{}), make(chan struct{})
	var executions atomic.Int64
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executions.Add(1)
		close(started)
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	handler := idempotencyTestHandler(t, store, downstream)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- performIdempotentRequest(handler, "first", "same-key", `{"title":"x"}`)
	}()
	<-started

	inProgress := performIdempotentRequest(handler, "second", "same-key", `{"title":"x"}`)
	require.Equal(t, http.StatusConflict, inProgress.Code)
	require.Equal(t, "1", inProgress.Header().Get("Retry-After"))
	requireProblemCode(t, inProgress, "IDEMPOTENCY_IN_PROGRESS")

	reused := performIdempotentRequest(handler, "third", "same-key", `{"title":"different"}`)
	require.Equal(t, http.StatusConflict, reused.Code)
	requireProblemCode(t, reused, "IDEMPOTENCY_KEY_REUSED")
	close(release)
	require.Equal(t, http.StatusCreated, (<-firstDone).Code)
	require.EqualValues(t, 1, executions.Load())
}

func TestIdempotencyRetainsFourHundredsAndReleasesFiveHundreds(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		wantExecutions int64
	}{
		{name: "client error retained", status: http.StatusUnprocessableEntity, wantExecutions: 1},
		{name: "server error released", status: http.StatusInternalServerError, wantExecutions: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var executions atomic.Int64
			handler := idempotencyTestHandler(
				t, newMemoryIdempotencyStore(),
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					executions.Add(1)
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte("result"))
				}),
			)
			first := performIdempotentRequest(handler, "first", "retention-key", `{}`)
			second := performIdempotentRequest(handler, "second", "retention-key", `{}`)
			require.Equal(t, test.status, first.Code)
			require.Equal(t, test.status, second.Code)
			require.Equal(t, test.wantExecutions, executions.Load())
		})
	}
}

func TestIdempotencyReleasesClaimAfterPanic(t *testing.T) {
	var executions atomic.Int64
	handler := idempotencyTestHandler(
		t, newMemoryIdempotencyStore(),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if executions.Add(1) == 1 {
				panic("downstream panic")
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	require.Panics(t, func() {
		performIdempotentRequest(handler, "first", "panic-key", `{}`)
	})
	retry := performIdempotentRequest(handler, "second", "panic-key", `{}`)
	require.Equal(t, http.StatusNoContent, retry.Code)
	require.EqualValues(t, 2, executions.Load())
}

func idempotencyTestHandler(
	t *testing.T,
	store idempotencyRepository,
	downstream http.Handler,
) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tasks", downstream)
	tokenID, userID := uuid.New(), uuid.New()
	principal := identity.RequestIdentity{
		Actor: domain.User{ID: userID, Active: true}, Subject: domain.User{ID: userID, Active: true},
		AuthenticationMethod: access.AuthenticationMethodAPIToken,
		APITokenID:           &tokenID, Scopes: []access.Scope{access.ScopeWorkWrite},
	}
	injectIdentity := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identity.WithRequestIdentity(r.Context(), principal)
		idempotencyMiddleware{store: store, routes: mux}.wrap(mux).ServeHTTP(w, r.WithContext(ctx))
	})
	return RequestIDMiddleware(injectIdentity)
}

func performIdempotentRequest(
	handler http.Handler,
	requestIDValue string,
	key string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks?sort=stable", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", requestIDValue)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireProblemCode(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, code, problem.Code)
}
