package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestV1GitLabDeliveryConnectsProjectAgentExecutionAndReviewSnapshot(t *testing.T) {
	handler, db := newTaskTestServer(t)
	repositoryPath := "team/api-delivery-" + uuid.NewString()
	repositoryURL := "https://gitlab.example/" + repositoryPath
	connectionResponse := do(
		t, handler, http.MethodPost, "/api/admin/gitlab-connections", userA,
		map[string]any{
			"label": "API delivery repository", "repository_url": repositoryURL,
			"credential": "synthetic-read-token",
		},
	)
	require.Equal(t, http.StatusCreated, connectionResponse.Code, connectionResponse.Body.String())
	var connection struct {
		ID uuid.UUID `json:"id"`
	}
	decodeJSON(t, connectionResponse, &connection)
	cleanupAPIGitLabConnection(t, db, connection.ID)
	var issuedTokenID uuid.UUID
	t.Cleanup(func() {
		if issuedTokenID == uuid.Nil {
			return
		}
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM business_audit_events WHERE token_id=$1`, issuedTokenID)
		require.NoError(t, err)
		cleanupAPIToken(t, db, issuedTokenID)
	})

	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "GitLab delivery transport",
	})
	require.Equal(t, http.StatusCreated, createdProject.Code, createdProject.Body.String())
	var project v1ProjectJSON
	decodeJSON(t, createdProject, &project)
	cleanupProjectRows(t, db, project.ID)
	projectPath := fmt.Sprintf("/api/v1/projects/%d", project.Number)

	bound := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/repositories", userA,
		http.Header{"If-Match": {`"1"`}}, map[string]any{"repository_url": repositoryURL},
	)
	require.Equal(t, http.StatusCreated, bound.Code, bound.Body.String())
	var binding struct {
		ProjectVersion int64 `json:"project_version"`
		Repository     struct {
			ID                  uuid.UUID `json:"id"`
			PathWithNamespace   string    `json:"path_with_namespace"`
			GitLabProjectID     int64     `json:"gitlab_project_id"`
			CanonicalRepository string    `json:"canonical_web_url"`
		} `json:"repository"`
	}
	decodeJSON(t, bound, &binding)
	require.Equal(t, int64(2), binding.ProjectVersion)
	require.Equal(t, repositoryPath, binding.Repository.PathWithNamespace)

	memberAdded := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/members", userA,
		http.Header{"If-Match": {`"2"`}}, map[string]any{"user_id": userB, "role": "member"},
	)
	require.Equal(t, http.StatusCreated, memberAdded.Code, memberAdded.Body.String())
	memberUnbind := doWithHeaders(
		t, handler, http.MethodDelete,
		fmt.Sprintf("%s/repositories/%s", projectPath, binding.Repository.ID), userB,
		http.Header{"If-Match": {`"3"`}}, nil,
	)
	require.Equal(t, http.StatusForbidden, memberUnbind.Code, memberUnbind.Body.String())

	tokenResponse := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name":   "delivery-executor-" + uuid.NewString(),
		"scopes": []string{"work:execute"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, tokenResponse.Code, tokenResponse.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, tokenResponse, &issued)
	issuedTokenID = issued.ID

	createdTask := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Freeze one Merge Request",
		"context":         "The review must receive verifiable delivery identity.",
		"expected_result": "Execution freezes the linked MR for review.",
		"project_number":  project.Number,
	})
	require.Equal(t, http.StatusCreated, createdTask.Code, createdTask.Body.String())
	var task struct {
		ID     uuid.UUID `json:"id"`
		Number int64     `json:"number"`
	}
	decodeJSON(t, createdTask, &task)
	taskPath := fmt.Sprintf("/api/v1/tasks/%d", task.Number)

	ready := doBearerMutation(
		t, handler, http.MethodPost, taskPath+"/commands/mark-ready", issued.Token,
		http.Header{"If-Match": {`"1"`}}, nil,
	)
	require.Equal(t, http.StatusOK, ready.Code, ready.Body.String())
	claimed := doBearerMutation(
		t, handler, http.MethodPost, taskPath+"/claims", issued.Token,
		http.Header{"If-Match": {`"2"`}}, nil,
	)
	require.Equal(t, http.StatusCreated, claimed.Code, claimed.Body.String())
	var execution stageClaimCommandJSON
	decodeJSON(t, claimed, &execution)

	mergeRequestURL := repositoryURL + "/-/merge_requests/42"
	linked := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/merge-requests", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"3"`}},
		map[string]any{"merge_request_url": mergeRequestURL},
	)
	require.Equal(t, http.StatusCreated, linked.Code, linked.Body.String())
	var linkMutation struct {
		Task         workflowJSON `json:"task"`
		MergeRequest struct {
			ID              uuid.UUID `json:"id"`
			MergeRequestIID int64     `json:"merge_request_iid"`
			WebURL          string    `json:"web_url"`
		} `json:"merge_request"`
	}
	decodeJSON(t, linked, &linkMutation)
	require.Equal(t, int64(4), linkMutation.Task.Version)
	require.Equal(t, execution.Claim.Version, int64(1))
	require.Equal(t, mergeRequestURL, linkMutation.MergeRequest.WebURL)
	outageMergeRequestURL := repositoryURL + "/-/merge_requests/503"
	linkedDuringOutage := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/merge-requests", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"4"`}},
		map[string]any{"merge_request_url": outageMergeRequestURL},
	)
	require.Equal(t, http.StatusCreated, linkedDuringOutage.Code, linkedDuringOutage.Body.String())

	delivery := doBearerRequest(
		t, handler, http.MethodGet, taskPath+"/merge-requests", issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, delivery.Code, delivery.Body.String())
	var beforeReview struct {
		ActiveLinks []struct {
			ID uuid.UUID `json:"id"`
		} `json:"active_links"`
	}
	decodeJSON(t, delivery, &beforeReview)
	require.Len(t, beforeReview.ActiveLinks, 2)

	completed := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/complete-execution", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"5"`}},
		map[string]any{"body": "MR delivery is ready for review."},
	)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var completion struct {
		Task       workflowJSON `json:"task"`
		Completion struct {
			ExecutionCompleted struct {
				ReviewCycle   int64 `json:"review_cycle"`
				MergeRequests []struct {
					TaskMergeRequestID uuid.UUID `json:"task_merge_request_id"`
					MergeRequestIID    int64     `json:"merge_request_iid"`
					HeadSHA            string    `json:"head_sha"`
					ObservationStatus  string    `json:"observation_status"`
				} `json:"merge_requests"`
			} `json:"execution_completed"`
		} `json:"completion"`
	}
	decodeJSON(t, completed, &completion)
	require.Equal(t, "in_review", completion.Task.Phase)
	require.Equal(t, int64(1), completion.Completion.ExecutionCompleted.ReviewCycle)
	require.Len(t, completion.Completion.ExecutionCompleted.MergeRequests, 2)
	require.Equal(
		t, linkMutation.MergeRequest.ID,
		completion.Completion.ExecutionCompleted.MergeRequests[0].TaskMergeRequestID,
	)
	require.Equal(t, "abc123def456", completion.Completion.ExecutionCompleted.MergeRequests[0].HeadSHA)
	require.Equal(t, int64(503), completion.Completion.ExecutionCompleted.MergeRequests[1].MergeRequestIID)
	require.Equal(t, "unreachable", completion.Completion.ExecutionCompleted.MergeRequests[1].ObservationStatus)
	require.Equal(t, "abc123def456", completion.Completion.ExecutionCompleted.MergeRequests[1].HeadSHA)

	reviewDelivery := doBearerRequest(
		t, handler, http.MethodGet, taskPath+"/merge-requests", issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, reviewDelivery.Code, reviewDelivery.Body.String())
	var reviewState struct {
		Review struct {
			ReviewCycle   int64 `json:"review_cycle"`
			MergeRequests []struct {
				Comparison string `json:"comparison"`
			} `json:"merge_requests"`
		} `json:"review"`
	}
	decodeJSON(t, reviewDelivery, &reviewState)
	require.Equal(t, int64(1), reviewState.Review.ReviewCycle)
	require.Len(t, reviewState.Review.MergeRequests, 2)
	require.Equal(t, "unchanged", reviewState.Review.MergeRequests[0].Comparison)
	require.Equal(t, "unreachable", reviewState.Review.MergeRequests[1].Comparison)

	reviewClaimed := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"6"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusCreated, reviewClaimed.Code, reviewClaimed.Body.String())
	var reviewClaim stageClaimCommandJSON
	decodeJSON(t, reviewClaimed, &reviewClaim)
	require.Equal(t, "review", reviewClaim.Claim.Stage)
	reviewLink := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/merge-requests", reviewClaim.Claim.ID),
		userA, http.Header{"If-Match": {`"7"`}},
		map[string]any{"merge_request_url": mergeRequestURL},
	)
	require.Equal(t, http.StatusConflict, reviewLink.Code, reviewLink.Body.String())
	var reviewProblem struct {
		Code string `json:"code"`
	}
	decodeJSON(t, reviewLink, &reviewProblem)
	require.Equal(t, "INVALID_TRANSITION", reviewProblem.Code)
}

func cleanupAPIGitLabConnection(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `DELETE FROM business_audit_events WHERE entity_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM gitlab_connection_events WHERE connection_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM gitlab_connections WHERE id=$1`, id)
		require.NoError(t, err)
	})
}
