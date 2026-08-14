package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type criterionJSON struct {
	ID       uuid.UUID `json:"id"`
	Version  int64     `json:"version"`
	Revision int       `json:"revision"`
}

func doBearerMutation(
	t *testing.T,
	h http.Handler,
	method, path, token string,
	headers http.Header,
	body any,
) *httptest.ResponseRecorder {
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Idempotency-Key", uuid.NewString())
	if headers.Get("Pactline-Client-Kind") == "" {
		headers.Set("Pactline-Client-Kind", "api-test")
	}
	if headers.Get("Pactline-Client-Session-ID") == "" {
		headers.Set("Pactline-Client-Session-ID", "test-"+uuid.NewString())
	}
	return doBearerRequest(t, h, method, path, token, headers, body)
}

func doBearerRequest(
	t *testing.T,
	h http.Handler,
	method, path, token string,
	headers http.Header,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&payload).Encode(body))
	}
	request := httptest.NewRequest(method, path, &payload)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}
