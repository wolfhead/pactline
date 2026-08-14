package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/wolfhead/pactline/internal/integrations/repositoryfixture"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestV1RepositoryDeliveryConnectsProjectAgentExecutionAndReviewSnapshot(t *testing.T) {
	handler, db := newTaskTestServer(t)
	repositoryPath := "team/api-delivery-" + uuid.NewString()
	repositoryURL := "https://gitlab.example/" + repositoryPath
	connectionResponse := do(
		t, handler, http.MethodPost, "/api/admin/repository-connections", userA,
		map[string]any{
			"label": "API delivery repository", "provider": "gitlab", "repository_url": repositoryURL,
			"credential": "synthetic-read-token",
		},
	)
	require.Equal(t, http.StatusCreated, connectionResponse.Code, connectionResponse.Body.String())
	var connection struct {
		ID uuid.UUID `json:"id"`
	}
	decodeJSON(t, connectionResponse, &connection)
	cleanupAPIRepositoryConnection(t, db, connection.ID)
	githubRepositoryPath := "team/github-delivery-" + uuid.NewString()
	githubRepositoryURL := "https://github.example/" + githubRepositoryPath
	githubConnectionResponse := do(
		t, handler, http.MethodPost, "/api/admin/repository-connections", userA,
		map[string]any{
			"label": "GitHub API delivery repository", "provider": "github",
			"repository_url": githubRepositoryURL, "credential": "synthetic-github-read-token",
		},
	)
	require.Equal(t, http.StatusCreated, githubConnectionResponse.Code, githubConnectionResponse.Body.String())
	var githubConnection struct {
		ID uuid.UUID `json:"id"`
	}
	decodeJSON(t, githubConnectionResponse, &githubConnection)
	cleanupAPIRepositoryConnection(t, db, githubConnection.ID)
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
		http.Header{"If-Match": {`"1"`}}, map[string]any{"repository_url": repositoryURL, "provider": "gitlab"},
	)
	require.Equal(t, http.StatusCreated, bound.Code, bound.Body.String())
	var binding struct {
		ProjectVersion int64 `json:"project_version"`
		Repository     struct {
			ID                  uuid.UUID `json:"id"`
			PathWithNamespace   string    `json:"path_with_namespace"`
			Provider            string    `json:"provider"`
			CanonicalRepository string    `json:"canonical_web_url"`
		} `json:"repository"`
	}
	decodeJSON(t, bound, &binding)
	require.Equal(t, int64(2), binding.ProjectVersion)
	require.Equal(t, "gitlab", binding.Repository.Provider)
	require.Equal(t, repositoryPath, binding.Repository.PathWithNamespace)
	githubBound := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/repositories", userA,
		http.Header{"If-Match": {`"2"`}}, map[string]any{"repository_url": githubRepositoryURL, "provider": "github"},
	)
	require.Equal(t, http.StatusCreated, githubBound.Code, githubBound.Body.String())
	var githubBinding struct {
		ProjectVersion int64 `json:"project_version"`
		Repository     struct {
			Provider string `json:"provider"`
		} `json:"repository"`
	}
	decodeJSON(t, githubBound, &githubBinding)
	require.Equal(t, int64(3), githubBinding.ProjectVersion)
	require.Equal(t, "github", githubBinding.Repository.Provider)

	memberAdded := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/members", userA,
		http.Header{"If-Match": {`"3"`}}, map[string]any{"user_id": userB, "role": "member"},
	)
	require.Equal(t, http.StatusCreated, memberAdded.Code, memberAdded.Body.String())
	memberUnbind := doWithHeaders(
		t, handler, http.MethodDelete,
		fmt.Sprintf("%s/repositories/%s", projectPath, binding.Repository.ID), userB,
		http.Header{"If-Match": {`"4"`}}, nil,
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
		"title":           "Freeze one code change",
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

	codeChangeURL := repositoryURL + "/-/merge_requests/42"
	linked := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/code-changes", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"3"`}},
		map[string]any{"code_change_url": codeChangeURL},
	)
	require.Equal(t, http.StatusCreated, linked.Code, linked.Body.String())
	var linkMutation struct {
		Task       workflowJSON `json:"task"`
		CodeChange struct {
			ID           uuid.UUID `json:"id"`
			Kind         string    `json:"kind"`
			ChangeNumber int64     `json:"change_number"`
			WebURL       string    `json:"web_url"`
		} `json:"code_change"`
	}
	decodeJSON(t, linked, &linkMutation)
	require.Equal(t, int64(4), linkMutation.Task.Version)
	require.Equal(t, execution.Claim.Version, int64(1))
	require.Equal(t, "merge_request", linkMutation.CodeChange.Kind)
	require.Equal(t, codeChangeURL, linkMutation.CodeChange.WebURL)
	githubCodeChangeURL := githubRepositoryURL + "/pull/17"
	githubLinked := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/code-changes", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"4"`}},
		map[string]any{"code_change_url": githubCodeChangeURL},
	)
	require.Equal(t, http.StatusCreated, githubLinked.Code, githubLinked.Body.String())
	var githubLinkMutation struct {
		CodeChange struct {
			Kind         string `json:"kind"`
			ChangeNumber int64  `json:"change_number"`
			WebURL       string `json:"web_url"`
		} `json:"code_change"`
	}
	decodeJSON(t, githubLinked, &githubLinkMutation)
	require.Equal(t, "pull_request", githubLinkMutation.CodeChange.Kind)
	require.Equal(t, int64(17), githubLinkMutation.CodeChange.ChangeNumber)
	require.Equal(t, githubCodeChangeURL, githubLinkMutation.CodeChange.WebURL)
	outageCodeChangeURL := repositoryURL + "/-/merge_requests/503"
	linkedDuringOutage := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/code-changes", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"5"`}},
		map[string]any{"code_change_url": outageCodeChangeURL},
	)
	require.Equal(t, http.StatusCreated, linkedDuringOutage.Code, linkedDuringOutage.Body.String())

	delivery := doBearerRequest(
		t, handler, http.MethodGet, taskPath+"/code-changes", issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, delivery.Code, delivery.Body.String())
	var beforeReview struct {
		ActiveLinks []struct {
			ID uuid.UUID `json:"id"`
		} `json:"active_links"`
	}
	decodeJSON(t, delivery, &beforeReview)
	require.Len(t, beforeReview.ActiveLinks, 3)

	completed := doBearerMutation(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/complete-execution", execution.Claim.ID),
		issued.Token, http.Header{"If-Match": {`"6"`}},
		map[string]any{"body": "Code-change delivery is ready for review."},
	)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var completion struct {
		Task       workflowJSON `json:"task"`
		Completion struct {
			ExecutionCompleted struct {
				ReviewCycle int64 `json:"review_cycle"`
				CodeChanges []struct {
					TaskCodeChangeID uuid.UUID `json:"task_code_change_id"`
					ChangeNumber     int64     `json:"change_number"`
					ProviderEvidence *struct {
						HeadSHA string `json:"head_sha"`
					} `json:"provider_evidence"`
				} `json:"code_changes"`
			} `json:"execution_completed"`
		} `json:"completion"`
	}
	decodeJSON(t, completed, &completion)
	require.Equal(t, "in_review", completion.Task.Phase)
	require.Equal(t, int64(1), completion.Completion.ExecutionCompleted.ReviewCycle)
	require.Len(t, completion.Completion.ExecutionCompleted.CodeChanges, 3)
	require.Equal(
		t, linkMutation.CodeChange.ID,
		completion.Completion.ExecutionCompleted.CodeChanges[1].TaskCodeChangeID,
	)
	require.Equal(t, int64(17), completion.Completion.ExecutionCompleted.CodeChanges[0].ChangeNumber)
	require.Equal(t, "fedcba654321", completion.Completion.ExecutionCompleted.CodeChanges[0].ProviderEvidence.HeadSHA)
	require.Equal(t, "abc123def456", completion.Completion.ExecutionCompleted.CodeChanges[1].ProviderEvidence.HeadSHA)
	require.Equal(t, int64(503), completion.Completion.ExecutionCompleted.CodeChanges[2].ChangeNumber)
	require.NotNil(t, completion.Completion.ExecutionCompleted.CodeChanges[2].ProviderEvidence)
	require.Equal(t, "abc123def456", completion.Completion.ExecutionCompleted.CodeChanges[2].ProviderEvidence.HeadSHA)

	reviewDelivery := doBearerRequest(
		t, handler, http.MethodGet, taskPath+"/code-changes", issued.Token, nil, nil,
	)
	require.Equal(t, http.StatusOK, reviewDelivery.Code, reviewDelivery.Body.String())
	var reviewState struct {
		Review struct {
			ReviewCycle int64 `json:"review_cycle"`
			CodeChanges []struct {
				Comparison string `json:"comparison"`
			} `json:"code_changes"`
		} `json:"review"`
	}
	decodeJSON(t, reviewDelivery, &reviewState)
	require.Equal(t, int64(1), reviewState.Review.ReviewCycle)
	require.Len(t, reviewState.Review.CodeChanges, 3)
	require.Equal(t, "unchanged", reviewState.Review.CodeChanges[0].Comparison)
	require.Equal(t, "unchanged", reviewState.Review.CodeChanges[1].Comparison)
	require.Equal(t, "unreachable", reviewState.Review.CodeChanges[2].Comparison)

	reviewClaimed := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"7"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusCreated, reviewClaimed.Code, reviewClaimed.Body.String())
	var reviewClaim stageClaimCommandJSON
	decodeJSON(t, reviewClaimed, &reviewClaim)
	require.Equal(t, "review", reviewClaim.Claim.Stage)
	reviewLink := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/code-changes", reviewClaim.Claim.ID),
		userA, http.Header{"If-Match": {`"8"`}},
		map[string]any{"code_change_url": codeChangeURL},
	)
	require.Equal(t, http.StatusConflict, reviewLink.Code, reviewLink.Body.String())
	var reviewProblem struct {
		Code string `json:"code"`
	}
	decodeJSON(t, reviewLink, &reviewProblem)
	require.Equal(t, "INVALID_TRANSITION", reviewProblem.Code)
}

func TestV1RepositoryDeliveryCompletesWithoutRepositoryConnection(t *testing.T) {
	handler, db := newTaskTestServer(t)
	var lateConnectionID uuid.UUID
	t.Cleanup(func() {
		if lateConnectionID == uuid.Nil {
			return
		}
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `DELETE FROM business_audit_events WHERE entity_id=$1`, lateConnectionID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connection_events WHERE connection_id=$1`, lateConnectionID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connections WHERE id=$1`, lateConnectionID)
		require.NoError(t, err)
	})
	repositoryURL := "https://github.com/pactline-test/no-connection-" + uuid.NewString()
	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "Connection-independent delivery",
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

	createdTask := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Accept unverified pull request",
		"context":         "Repository membership must work without provider credentials.",
		"expected_result": "The Task reaches done with a frozen pull-request URL.",
		"project_number":  project.Number,
	})
	require.Equal(t, http.StatusCreated, createdTask.Code, createdTask.Body.String())
	var task struct {
		Number int64 `json:"number"`
	}
	decodeJSON(t, createdTask, &task)
	taskPath := fmt.Sprintf("/api/v1/tasks/%d", task.Number)

	ready := doWithHeaders(t, handler, http.MethodPost, taskPath+"/commands/mark-ready", userA,
		http.Header{"If-Match": {`"1"`}}, nil)
	require.Equal(t, http.StatusOK, ready.Code, ready.Body.String())
	claimed := doWithHeaders(t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"2"`}}, map[string]any{})
	require.Equal(t, http.StatusCreated, claimed.Code, claimed.Body.String())
	var execution stageClaimCommandJSON
	decodeJSON(t, claimed, &execution)

	codeChangeURL := repositoryURL + "/pull/42"
	linked := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/code-changes", execution.Claim.ID), userA,
		http.Header{"If-Match": {`"3"`}}, map[string]any{"code_change_url": codeChangeURL},
	)
	require.Equal(t, http.StatusCreated, linked.Code, linked.Body.String())
	var linkPayload struct {
		CodeChange map[string]any `json:"code_change"`
	}
	decodeJSON(t, linked, &linkPayload)
	require.Equal(t, codeChangeURL, linkPayload.CodeChange["web_url"])
	require.NotContains(t, linkPayload.CodeChange, "provider_evidence")
	require.NotContains(t, linkPayload.CodeChange, "provider_verification")

	completed := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/complete-execution", execution.Claim.ID), userA,
		http.Header{"If-Match": {`"4"`}}, map[string]any{"body": "URL delivery is ready for review."},
	)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var completion struct {
		Task       workflowJSON `json:"task"`
		Completion struct {
			ExecutionCompleted struct {
				CodeChanges []struct {
					WebURL           string         `json:"web_url"`
					ProviderEvidence map[string]any `json:"provider_evidence"`
				} `json:"code_changes"`
			} `json:"execution_completed"`
		} `json:"completion"`
	}
	decodeJSON(t, completed, &completion)
	require.Equal(t, "in_review", completion.Task.Phase)
	require.Len(t, completion.Completion.ExecutionCompleted.CodeChanges, 1)
	require.Equal(t, codeChangeURL, completion.Completion.ExecutionCompleted.CodeChanges[0].WebURL)
	require.Nil(t, completion.Completion.ExecutionCompleted.CodeChanges[0].ProviderEvidence)

	reviewClaimed := doWithHeaders(t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"5"`}}, map[string]any{})
	require.Equal(t, http.StatusCreated, reviewClaimed.Code, reviewClaimed.Body.String())
	var review stageClaimCommandJSON
	decodeJSON(t, reviewClaimed, &review)
	accepted := doWithHeaders(
		t, handler, http.MethodPost, fmt.Sprintf("/api/v1/claims/%s/accept", review.Claim.ID), userA,
		http.Header{"If-Match": {`"6"`}}, map[string]any{"body": "Unverified provider evidence is acceptable."},
	)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	var acceptance struct {
		Task workflowJSON `json:"task"`
	}
	decodeJSON(t, accepted, &acceptance)
	require.Equal(t, "done", acceptance.Task.Phase)

	connectionResponse := do(
		t, handler, http.MethodPost, "/api/admin/repository-connections", userA,
		map[string]any{
			"label": "Late evidence connection", "provider": "github",
			"repository_url": repositoryURL, "credential": "synthetic-read-token",
		},
	)
	require.Equal(t, http.StatusCreated, connectionResponse.Code, connectionResponse.Body.String())
	var connection struct {
		ID uuid.UUID `json:"id"`
	}
	decodeJSON(t, connectionResponse, &connection)
	lateConnectionID = connection.ID

	delivery := doWithHeaders(t, handler, http.MethodGet, taskPath+"/code-changes", userA, nil, nil)
	require.Equal(t, http.StatusOK, delivery.Code, delivery.Body.String())
	var enriched struct {
		ActiveLinks []struct {
			ProviderEvidence *struct {
				HeadSHA string `json:"head_sha"`
			} `json:"provider_evidence"`
		} `json:"active_links"`
		Review struct {
			CodeChanges []struct {
				Comparison string `json:"comparison"`
			} `json:"code_changes"`
		} `json:"review"`
	}
	decodeJSON(t, delivery, &enriched)
	require.Len(t, enriched.ActiveLinks, 1)
	require.NotNil(t, enriched.ActiveLinks[0].ProviderEvidence)
	require.Equal(t, "fedcba654321", enriched.ActiveLinks[0].ProviderEvidence.HeadSHA)
	require.Equal(t, "unverified", enriched.Review.CodeChanges[0].Comparison,
		"late enrichment must not rewrite the frozen completion snapshot")
}

func TestV1RepositoryFixturesCompleteMixedProviderDelivery(t *testing.T) {
	handler, db := newTaskTestServer(t)
	connectionIDs := make([]uuid.UUID, 0, 2)
	for _, input := range []map[string]any{
		{
			"label": "GitHub fixture", "provider": "github",
			"repository_url": repositoryfixture.GitHubOrigin + "/" + repositoryfixture.RepositoryPath,
			"credential":     repositoryfixture.SyntheticCredential,
		},
		{
			"label": "GitLab fixture", "provider": "gitlab",
			"repository_url": repositoryfixture.GitLabOrigin + "/" + repositoryfixture.RepositoryPath,
			"credential":     repositoryfixture.SyntheticCredential,
		},
	} {
		response := do(t, handler, http.MethodPost, "/api/admin/repository-connections", userA, input)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var connection struct {
			ID uuid.UUID `json:"id"`
		}
		decodeJSON(t, response, &connection)
		connectionIDs = append(connectionIDs, connection.ID)
		cleanupAPIRepositoryConnection(t, db, connection.ID)
	}

	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "Repository fixture acceptance",
	})
	require.Equal(t, http.StatusCreated, createdProject.Code, createdProject.Body.String())
	var project v1ProjectJSON
	decodeJSON(t, createdProject, &project)
	cleanupProjectRows(t, db, project.ID)
	projectPath := fmt.Sprintf("/api/v1/projects/%d", project.Number)
	for index, repositoryURL := range []string{
		repositoryfixture.GitHubOrigin + "/" + repositoryfixture.RepositoryPath,
		repositoryfixture.GitLabOrigin + "/" + repositoryfixture.RepositoryPath,
	} {
		bound := doWithHeaders(
			t, handler, http.MethodPost, projectPath+"/repositories", userA,
			http.Header{"If-Match": {fmt.Sprintf(`"%d"`, index+1)}},
			map[string]any{
				"repository_url": repositoryURL,
				"provider":       []string{"github", "gitlab"}[index],
			},
		)
		require.Equal(t, http.StatusCreated, bound.Code, bound.Body.String())
	}

	createdTask := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Complete fixture-backed delivery",
		"context":         "Exercise both repository providers without external services.",
		"expected_result": "Review receives one frozen PR and one frozen MR.",
		"project_number":  project.Number,
	})
	require.Equal(t, http.StatusCreated, createdTask.Code, createdTask.Body.String())
	var task struct {
		Number int64 `json:"number"`
	}
	decodeJSON(t, createdTask, &task)
	taskPath := fmt.Sprintf("/api/v1/tasks/%d", task.Number)

	ready := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/commands/mark-ready", userA,
		http.Header{"If-Match": {`"1"`}}, nil,
	)
	require.Equal(t, http.StatusOK, ready.Code, ready.Body.String())
	claimed := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"2"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusCreated, claimed.Code, claimed.Body.String())
	var execution stageClaimCommandJSON
	decodeJSON(t, claimed, &execution)

	for index, codeChangeURL := range []string{
		repositoryfixture.GitHubOrigin + "/" + repositoryfixture.RepositoryPath + "/pull/42",
		repositoryfixture.GitLabOrigin + "/" + repositoryfixture.RepositoryPath + "/-/merge_requests/43",
	} {
		linked := doWithHeaders(
			t, handler, http.MethodPost,
			fmt.Sprintf("/api/v1/claims/%s/code-changes", execution.Claim.ID),
			userA, http.Header{"If-Match": {fmt.Sprintf(`"%d"`, index+3)}},
			map[string]any{"code_change_url": codeChangeURL},
		)
		require.Equal(t, http.StatusCreated, linked.Code, linked.Body.String())
	}

	completed := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/complete-execution", execution.Claim.ID),
		userA, http.Header{"If-Match": {`"5"`}},
		map[string]any{"body": "Both fixture code changes are ready for review."},
	)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var completion struct {
		Task       workflowJSON `json:"task"`
		Completion struct {
			ExecutionCompleted struct {
				CodeChanges []struct {
					Provider         string `json:"provider"`
					ProviderEvidence struct {
						HeadSHA string `json:"head_sha"`
					} `json:"provider_evidence"`
				} `json:"code_changes"`
			} `json:"execution_completed"`
		} `json:"completion"`
	}
	decodeJSON(t, completed, &completion)
	require.Equal(t, "in_review", completion.Task.Phase)
	require.Equal(t, []string{"github", "gitlab"}, []string{
		completion.Completion.ExecutionCompleted.CodeChanges[0].Provider,
		completion.Completion.ExecutionCompleted.CodeChanges[1].Provider,
	})
	require.Equal(t, "1111111111111111111111111111111111111111", completion.Completion.ExecutionCompleted.CodeChanges[0].ProviderEvidence.HeadSHA)
	require.Equal(t, "2222222222222222222222222222222222222222", completion.Completion.ExecutionCompleted.CodeChanges[1].ProviderEvidence.HeadSHA)

	reviewClaimed := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/claims", userA,
		http.Header{"If-Match": {`"6"`}}, map[string]any{},
	)
	require.Equal(t, http.StatusCreated, reviewClaimed.Code, reviewClaimed.Body.String())
	var review stageClaimCommandJSON
	decodeJSON(t, reviewClaimed, &review)
	require.Equal(t, "review", review.Claim.Stage)
	accepted := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/claims/%s/accept", review.Claim.ID),
		userA, http.Header{"If-Match": {`"7"`}},
		map[string]any{"body": "Fixture delivery snapshot is accepted."},
	)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	var acceptance struct {
		Task workflowJSON `json:"task"`
	}
	decodeJSON(t, accepted, &acceptance)
	require.Equal(t, "done", acceptance.Task.Phase)
	require.Len(t, connectionIDs, 2)
}

func cleanupAPIRepositoryConnection(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `DELETE FROM business_audit_events WHERE entity_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connection_events WHERE connection_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connections WHERE id=$1`, id)
		require.NoError(t, err)
	})
}
