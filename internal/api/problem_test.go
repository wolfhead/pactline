package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteProblemEmitsRFC9457MediaTypeAndExtensions(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, Problem{
			Type:   "https://bountyboard.dev/problems/validation",
			Title:  "Validation failed",
			Status: http.StatusBadRequest,
			Detail: "name is required",
			Code:   "validation_failed",
			Errors: []ValidationProblem{{Pointer: "/name", Detail: "is required"}},
		})
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/tokens", nil)
	request.Header.Set("X-Request-ID", "agent-run_2026-07-28")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	var got Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &got))
	require.Equal(t, "validation_failed", got.Code)
	require.Equal(t, "agent-run_2026-07-28", got.RequestID)
	require.Equal(t, "/api/v1/account/tokens", got.Instance)
	require.Equal(t, []ValidationProblem{{Pointer: "/name", Detail: "is required"}}, got.Errors)
}
