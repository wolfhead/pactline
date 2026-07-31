package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type taskClaimJSON struct {
	ID         uuid.UUID `json:"id"`
	TaskNumber int64     `json:"task_number"`
	Status     string    `json:"status"`
	Version    int64     `json:"version"`
}

type taskClaimActionJSON struct {
	Claim taskClaimJSON `json:"claim"`
}

type criterionJSON struct {
	ID       uuid.UUID `json:"id"`
	Version  int64     `json:"version"`
	Revision int       `json:"revision"`
}

func TestV1ExecutorTokenRunsClaimWorkflowWithoutGeneralWriteAccess(t *testing.T) {
	handler, db := newTaskTestServer(t)

	tokenResponse := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name":   "claim-executor-" + uuid.NewString(),
		"scopes": []string{"work:execute"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, tokenResponse.Code, tokenResponse.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, tokenResponse, &issued)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })
	t.Cleanup(func() {
		_, err := db.Pool.Exec(
			context.Background(),
			`DELETE FROM business_audit_events WHERE token_id=$1`,
			issued.ID,
		)
		require.NoError(t, err)
	})

	taskResponse := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Executor transport workflow",
		"context":         "A third-party Codex session needs an eligible task.",
		"expected_result": "The session submits verified work for human review.",
		"project_number":  activeProjectNumber(t, db),
		"assignee_id":     userA,
		"execution_mode":  "agent_allowed",
	})
	require.Equal(t, http.StatusCreated, taskResponse.Code, taskResponse.Body.String())
	var task v1TaskJSON
	decodeJSON(t, taskResponse, &task)
	require.Equal(t, "agent_allowed", task.ExecutionMode)
	cleanupTaskRow(t, db, task.ID)

	criterionResponse := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/criteria", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion":                 "The executor transport workflow passes",
			"verification_instructions": "Run the focused API integration test",
			"position":                  0,
		},
	)
	require.Equal(t, http.StatusCreated, criterionResponse.Code, criterionResponse.Body.String())
	var criterion criterionJSON
	decodeJSON(t, criterionResponse, &criterion)

	session := map[string]any{
		"client_kind": "codex", "client_session_id": "thread-" + uuid.NewString(),
	}
	claimed := doBearerMutation(
		t, handler, http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/claim",
		issued.Token, nil, session,
	)
	require.Equal(t, http.StatusCreated, claimed.Code, claimed.Body.String())
	require.Equal(t, `"1"`, claimed.Header().Get("ETag"))
	var claimPayload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(claimed.Body.Bytes(), &claimPayload))
	require.NotContains(t, claimPayload, "client_session_id")
	var claim taskClaimJSON
	decodeJSON(t, claimed, &claim)
	require.Equal(t, task.Number, claim.TaskNumber)
	require.Equal(t, "active", claim.Status)

	current := doBearerRequest(
		t, handler, http.MethodGet,
		fmt.Sprintf(
			"/api/v1/agent/claims/current?client_kind=codex&client_session_id=%s",
			session["client_session_id"],
		),
		issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var currentPayload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &currentPayload))
	require.NotContains(t, currentPayload, "client_session_id")

	forbiddenPatch := doBearerMutation(
		t, handler, http.MethodPatch,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10),
		issued.Token, http.Header{"If-Match": {`"3"`}},
		map[string]any{"title": "Executor must not edit the task brief"},
	)
	require.Equal(t, http.StatusForbidden, forbiddenPatch.Code, forbiddenPatch.Body.String())

	checked := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf(
			"/api/v1/claims/%s/criteria/%s/checks",
			claim.ID, criterion.ID,
		),
		issued.Token, http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"client_kind":        session["client_kind"],
			"client_session_id":  session["client_session_id"],
			"criterion_revision": criterion.Revision,
			"outcome":            "passed",
			"evidence":           "The focused API integration test passed.",
		},
	)
	require.Equal(t, http.StatusCreated, checked.Code, checked.Body.String())

	questionBody := map[string]any{
		"client_kind":       session["client_kind"],
		"client_session_id": session["client_session_id"],
		"body":              "Which compatibility target should be used?",
	}
	asked := doBearerMutation(
		t, handler, http.MethodPost,
		"/api/v1/claims/"+claim.ID.String()+"/ask",
		issued.Token, http.Header{"If-Match": {`"1"`}}, questionBody,
	)
	require.Equal(t, http.StatusOK, asked.Code, asked.Body.String())
	var askedAction taskClaimActionJSON
	decodeJSON(t, asked, &askedAction)
	require.Equal(t, "waiting_human", askedAction.Claim.Status)
	require.Equal(t, int64(2), askedAction.Claim.Version)

	answered := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/claims/"+claim.ID.String()+"/answer", userA,
		http.Header{"If-Match": {`"2"`}},
		map[string]any{"body": "Use the currently supported stable target."},
	)
	require.Equal(t, http.StatusOK, answered.Code, answered.Body.String())
	var answeredAction taskClaimActionJSON
	decodeJSON(t, answered, &answeredAction)
	require.Equal(t, "active", answeredAction.Claim.Status)
	require.Equal(t, int64(3), answeredAction.Claim.Version)

	submitted := doBearerMutation(
		t, handler, http.MethodPost,
		"/api/v1/claims/"+claim.ID.String()+"/submit",
		issued.Token, http.Header{"If-Match": {`"3"`}},
		map[string]any{
			"client_kind":       session["client_kind"],
			"client_session_id": session["client_session_id"],
			"body":              "Implemented the requested change and ran the focused test suite.",
		},
	)
	require.Equal(t, http.StatusOK, submitted.Code, submitted.Body.String())
	var submittedAction taskClaimActionJSON
	decodeJSON(t, submitted, &submittedAction)
	require.Equal(t, "submitted", submittedAction.Claim.Status)

	taskAfter := do(
		t, handler, http.MethodGet,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10), userA, nil,
	)
	require.Equal(t, http.StatusOK, taskAfter.Code, taskAfter.Body.String())
	var taskState struct {
		Status string `json:"status"`
	}
	decodeJSON(t, taskAfter, &taskState)
	require.Equal(t, "in_review", taskState.Status)

	conversations := do(
		t, handler, http.MethodGet,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/agent-conversations",
		userA, nil,
	)
	require.Equal(t, http.StatusOK, conversations.Code, conversations.Body.String())
	var timeline struct {
		Items []struct {
			Claim    map[string]json.RawMessage `json:"claim"`
			Messages []json.RawMessage          `json:"messages"`
		} `json:"items"`
	}
	decodeJSON(t, conversations, &timeline)
	require.Len(t, timeline.Items, 1)
	require.NotContains(t, timeline.Items[0].Claim, "client_session_id")
	require.Len(t, timeline.Items[0].Messages, 3)
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
