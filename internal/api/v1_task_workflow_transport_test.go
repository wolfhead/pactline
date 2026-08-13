package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type workflowJSON struct {
	TaskID              uuid.UUID  `json:"task_id"`
	TaskNumber          int64      `json:"task_number"`
	Version             int64      `json:"version"`
	Phase               string     `json:"phase"`
	Activity            string     `json:"activity"`
	ReviewCycle         int64      `json:"review_cycle"`
	ActiveIssueThreadID *uuid.UUID `json:"active_issue_thread_id"`
	MainThreadID        uuid.UUID  `json:"main_thread_id"`
}

type stageClaimJSON struct {
	ID        uuid.UUID `json:"id"`
	Stage     string    `json:"stage"`
	Status    string    `json:"status"`
	Outcome   string    `json:"outcome"`
	Version   int64     `json:"version"`
	ClaimedBy struct {
		Type string `json:"type"`
		Ref  string `json:"ref"`
	} `json:"claimed_by"`
}

type stageClaimCommandJSON struct {
	Task  workflowJSON   `json:"task"`
	Claim stageClaimJSON `json:"claim"`
}

func TestV1TaskWorkflowUsesCommandsForAgentAndHumanActors(t *testing.T) {
	handler, db := newTaskTestServer(t)
	tokenResponse := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name":   "workflow-executor-" + uuid.NewString(),
		"scopes": []string{"work:execute"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, tokenResponse.Code, tokenResponse.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, tokenResponse, &issued)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM business_audit_events WHERE token_id=$1`, issued.ID)
		require.NoError(t, err)
	})

	created := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Command-driven Task workflow",
		"context":         "People and Agents need one lifecycle.",
		"expected_result": "The Task reaches done through execution and acceptance Claims.",
		"project_number":  activeProjectNumber(t, db),
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var task struct {
		ID           uuid.UUID `json:"id"`
		Number       int64     `json:"number"`
		Version      int64     `json:"version"`
		Phase        string    `json:"phase"`
		ReviewCycle  int64     `json:"review_cycle"`
		MainThreadID uuid.UUID `json:"main_thread_id"`
	}
	decodeJSON(t, created, &task)
	cleanupTaskRow(t, db, task.ID)
	require.Equal(t, "backlog", task.Phase)
	require.NotEqual(t, uuid.Nil, task.MainThreadID)

	criterionResponse := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/criteria", task.Number), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion":                 "The command workflow reaches the expected result",
			"verification_instructions": "Inspect the final Task workflow state",
			"position":                  0,
		},
	)
	require.Equal(t, http.StatusCreated, criterionResponse.Code, criterionResponse.Body.String())
	var criterion criterionJSON
	decodeJSON(t, criterionResponse, &criterion)

	ready := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/mark-ready", task.Number), issued.Token,
		http.Header{"If-Match": {`"2"`}}, nil,
	)
	require.Equal(t, http.StatusOK, ready.Code, ready.Body.String())
	var readyWorkflow workflowJSON
	decodeJSON(t, ready, &readyWorkflow)
	require.Equal(t, "ready", readyWorkflow.Phase)

	invalidTransition := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/mark-ready", task.Number), issued.Token,
		http.Header{"If-Match": {`"3"`}}, nil,
	)
	require.Equal(t, http.StatusConflict, invalidTransition.Code, invalidTransition.Body.String())
	var transitionProblem struct {
		Code string `json:"code"`
	}
	decodeJSON(t, invalidTransition, &transitionProblem)
	require.Equal(t, "INVALID_TRANSITION", transitionProblem.Code)

	sessionID := "thread-" + uuid.NewString()
	claimed := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims", task.Number), issued.Token,
		http.Header{"If-Match": {`"3"`}},
		map[string]any{"client_kind": "codex", "client_session_id": sessionID},
	)
	require.Equal(t, http.StatusCreated, claimed.Code, claimed.Body.String())
	var execution stageClaimCommandJSON
	decodeJSON(t, claimed, &execution)
	require.Equal(t, "in_progress", execution.Task.Phase)
	require.Equal(t, "working", execution.Task.Activity)
	require.Equal(t, "execution", execution.Claim.Stage)
	require.Equal(t, "agent", execution.Claim.ClaimedBy.Type)
	require.NotContains(t, claimed.Body.String(), "client_session_id")

	duplicateClaim := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims", task.Number), userA,
		http.Header{"If-Match": {`"4"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusConflict, duplicateClaim.Code, duplicateClaim.Body.String())
	var claimProblem struct {
		Code string `json:"code"`
	}
	decodeJSON(t, duplicateClaim, &claimProblem)
	require.Equal(t, "ACTIVE_CLAIM", claimProblem.Code)

	progress := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/threads/%s/items", task.MainThreadID), issued.Token,
		nil,
		map[string]any{"kind": "progress", "body": "Focused execution checks are green."},
	)
	require.Equal(t, http.StatusCreated, progress.Code, progress.Body.String())
	var progressItem struct {
		Kind   string `json:"kind"`
		Author struct {
			Type string `json:"type"`
		} `json:"author"`
	}
	decodeJSON(t, progress, &progressItem)
	require.Equal(t, "progress", progressItem.Kind)
	require.Equal(t, "agent", progressItem.Author.Type)

	current := doBearerRequest(
		t, handler, http.MethodGet,
		fmt.Sprintf(
			"/api/v1/claims/current?client_kind=codex&client_session_id=%s",
			sessionID,
		),
		issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var currentClaim stageClaimJSON
	decodeJSON(t, current, &currentClaim)
	require.Equal(t, execution.Claim.ID, currentClaim.ID)

	resolutionRequested := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf(
			"/api/v1/tasks/%d/claims/%s/request-resolution",
			task.Number, execution.Claim.ID,
		),
		issued.Token, http.Header{"If-Match": {`"4"`}},
		map[string]any{
			"claim_version": execution.Claim.Version,
			"issue_type":    "decision_required",
			"request":       "Choose whether the continuation should retain compatibility behavior.",
		},
	)
	require.Equal(t, http.StatusOK, resolutionRequested.Code, resolutionRequested.Body.String())
	var blocked struct {
		Task  workflowJSON   `json:"task"`
		Claim stageClaimJSON `json:"claim"`
		Issue struct {
			ID      uuid.UUID `json:"id"`
			Version int64     `json:"version"`
		} `json:"issue"`
	}
	decodeJSON(t, resolutionRequested, &blocked)
	require.Equal(t, "needs_resolution", blocked.Task.Activity)
	require.Equal(t, "needs_resolution", blocked.Claim.Outcome)

	resolved := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/issues/%s/resolve", task.Number, blocked.Issue.ID),
		userA, http.Header{"If-Match": {`"5"`}},
		map[string]any{
			"thread_version": blocked.Issue.Version,
			"resolution":     "Retain compatibility behavior for this release.",
		},
	)
	require.Equal(t, http.StatusOK, resolved.Code, resolved.Body.String())

	endedCurrent := doBearerRequest(
		t, handler, http.MethodGet,
		fmt.Sprintf(
			"/api/v1/claims/current?client_kind=codex&client_session_id=%s",
			sessionID,
		),
		issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusNotFound, endedCurrent.Code, endedCurrent.Body.String())

	continuationSessionID := "thread-continuation-" + uuid.NewString()
	continued := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims", task.Number), issued.Token,
		http.Header{"If-Match": {`"6"`}},
		map[string]any{
			"client_kind": "codex", "client_session_id": continuationSessionID,
		},
	)
	require.Equal(t, http.StatusCreated, continued.Code, continued.Body.String())
	decodeJSON(t, continued, &execution)
	require.Equal(t, "execution", execution.Claim.Stage)
	require.NotEqual(t, blocked.Claim.ID, execution.Claim.ID)

	forbiddenPatch := doBearerMutation(
		t, handler, http.MethodPatch,
		fmt.Sprintf("/api/v1/tasks/%d", task.Number), issued.Token,
		http.Header{"If-Match": {`"7"`}},
		map[string]any{"title": "Executor must not edit the Task brief"},
	)
	require.Equal(t, http.StatusForbidden, forbiddenPatch.Code, forbiddenPatch.Body.String())

	checkedExecution := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf(
			"/api/v1/tasks/%d/claims/%s/criteria/%s/checks",
			task.Number, execution.Claim.ID, criterion.ID,
		),
		issued.Token, http.Header{"If-Match": {`"7"`}},
		map[string]any{
			"claim_version":      execution.Claim.Version,
			"criterion_revision": criterion.Revision,
			"outcome":            "passed", "evidence": "Focused command test passed.",
		},
	)
	require.Equal(t, http.StatusCreated, checkedExecution.Code, checkedExecution.Body.String())

	submitted := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims/%s/submit", task.Number, execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"7"`}},
		map[string]any{
			"claim_version": execution.Claim.Version,
			"body":          "Execution is complete and verified.",
		},
	)
	require.Equal(t, http.StatusOK, submitted.Code, submitted.Body.String())
	var submission stageClaimCommandJSON
	decodeJSON(t, submitted, &submission)
	require.Equal(t, "in_review", submission.Task.Phase)
	require.Equal(t, "available", submission.Task.Activity)
	require.Equal(t, int64(1), submission.Task.ReviewCycle)

	reviewClaimed := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims", task.Number), userA,
		http.Header{"If-Match": {`"8"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusCreated, reviewClaimed.Code, reviewClaimed.Body.String())
	var review stageClaimCommandJSON
	decodeJSON(t, reviewClaimed, &review)
	require.Equal(t, "review", review.Claim.Stage)
	require.Equal(t, "user", review.Claim.ClaimedBy.Type)

	reviewCheck := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf(
			"/api/v1/tasks/%d/claims/%s/criteria/%s/checks",
			task.Number, review.Claim.ID, criterion.ID,
		), userA, http.Header{"If-Match": {`"9"`}},
		map[string]any{
			"claim_version":      review.Claim.Version,
			"criterion_revision": criterion.Revision,
			"outcome":            "passed", "evidence": "Acceptance independently confirmed.",
		},
	)
	require.Equal(t, http.StatusCreated, reviewCheck.Code, reviewCheck.Body.String())

	accepted := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/claims/%s/accept", task.Number, review.Claim.ID),
		userA, http.Header{"If-Match": {`"9"`}},
		map[string]any{
			"claim_version": review.Claim.Version,
			"body":          "The Task acceptance contract is satisfied.",
		},
	)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	var acceptance stageClaimCommandJSON
	decodeJSON(t, accepted, &acceptance)
	require.Equal(t, "done", acceptance.Task.Phase)
	require.Empty(t, acceptance.Task.Activity)
	require.Equal(t, "task_accepted", acceptance.Claim.Outcome)

	threads := do(
		t, handler, http.MethodGet,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/threads", userA, nil,
	)
	require.Equal(t, http.StatusOK, threads.Code, threads.Body.String())

	createdForAdministrativeCommands := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Agent administrative workflow commands",
		"context":         "People and Agents share Task workflow capabilities.",
		"expected_result": "The Agent can withdraw readiness and cancel a Task.",
		"project_number":  activeProjectNumber(t, db),
	})
	require.Equal(t, http.StatusCreated, createdForAdministrativeCommands.Code, createdForAdministrativeCommands.Body.String())
	var commandTask struct {
		ID     uuid.UUID `json:"id"`
		Number int64     `json:"number"`
	}
	decodeJSON(t, createdForAdministrativeCommands, &commandTask)
	cleanupTaskRow(t, db, commandTask.ID)

	markedReady := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/mark-ready", commandTask.Number), issued.Token,
		http.Header{"If-Match": {`"1"`}}, nil,
	)
	require.Equal(t, http.StatusOK, markedReady.Code, markedReady.Body.String())

	withdrawn := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/withdraw-readiness", commandTask.Number), issued.Token,
		http.Header{"If-Match": {`"2"`}}, map[string]any{"reason": "The brief needs another pass."},
	)
	require.Equal(t, http.StatusOK, withdrawn.Code, withdrawn.Body.String())

	markedReadyAgain := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/mark-ready", commandTask.Number), issued.Token,
		http.Header{"If-Match": {`"3"`}}, nil,
	)
	require.Equal(t, http.StatusOK, markedReadyAgain.Code, markedReadyAgain.Body.String())

	cancelled := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/tasks/%d/commands/cancel", commandTask.Number), issued.Token,
		http.Header{"If-Match": {`"4"`}}, map[string]any{"reason": "The Task is no longer required."},
	)
	require.Equal(t, http.StatusOK, cancelled.Code, cancelled.Body.String())
}

func TestV1TaskReadRejectsUnclassifiedLegacyRows(t *testing.T) {
	handler, db := newTaskTestServer(t)
	created := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Legacy-null Task",
		"context":         "The target API must not invent lifecycle state.",
		"expected_result": "The API returns a stable migration-required problem.",
		"project_number":  activeProjectNumber(t, db),
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var task struct {
		ID     uuid.UUID `json:"id"`
		Number int64     `json:"number"`
	}
	decodeJSON(t, created, &task)
	cleanupTaskRow(t, db, task.ID)

	_, err := db.Pool.Exec(context.Background(), `
		UPDATE tasks
		SET phase=NULL,activity_state=NULL,review_cycle=NULL
		WHERE id=$1`, task.ID)
	require.NoError(t, err)

	response := do(
		t, handler, http.MethodGet,
		fmt.Sprintf("/api/v1/tasks/%d", task.Number), userA, nil,
	)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var problem struct {
		Code string `json:"code"`
	}
	decodeJSON(t, response, &problem)
	require.Equal(t, "MIGRATION_REQUIRED", problem.Code)
}
