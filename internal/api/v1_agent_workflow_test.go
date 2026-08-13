package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/stretchr/testify/require"
)

type agentTokenSecurity string

func (s agentTokenSecurity) BearerAuth(
	context.Context,
	generated.OperationName,
) (generated.BearerAuth, error) {
	return generated.BearerAuth{Token: string(s)}, nil
}

func (agentTokenSecurity) SessionCookie(
	context.Context,
	generated.OperationName,
) (generated.SessionCookie, error) {
	return generated.SessionCookie{}, ogenerrors.ErrSkipClientSecurity
}

func TestGeneratedClientAgentWorkflow(t *testing.T) {
	handler, db := newTaskTestServer(t)

	tokenResponse := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name":   "generated-agent-" + uuid.NewString(),
		"scopes": []string{"work:write", "work:execute"}, "expires_in_days": 30,
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

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := generated.NewClient(server.URL, agentTokenSecurity(issued.Token))
	require.NoError(t, err)
	ctx := context.Background()

	projectRequest := &generated.ProjectCreate{Name: "Generated Agent workflow"}
	projectKey := generated.NewOptString("create-project-" + uuid.NewString())
	projectResult, err := client.CreateProject(
		ctx, projectRequest,
		generated.CreateProjectParams{IdempotencyKey: projectKey},
	)
	require.NoError(t, err)
	projectCreated, ok := projectResult.(*generated.ProjectCreatedHeaders)
	require.True(t, ok, "unexpected create Project response %T", projectResult)
	project := projectCreated.Response
	cleanupProjectRows(t, db, project.ID)
	require.Equal(t, `"1"`, projectCreated.Etag.Or(""))

	projectReplayResult, err := client.CreateProject(
		ctx, projectRequest,
		generated.CreateProjectParams{IdempotencyKey: projectKey},
	)
	require.NoError(t, err)
	projectReplay, ok := projectReplayResult.(*generated.ProjectCreatedHeaders)
	require.True(t, ok, "unexpected Project replay response %T", projectReplayResult)
	require.Equal(t, project.ID, projectReplay.Response.ID)
	require.True(t, projectReplay.IdempotencyReplayed.Or(false))

	milestoneResult, err := client.CreateMilestone(
		ctx,
		&generated.MilestoneCreate{
			Name: "Agent workflow verified", Outcome: "All work is checked",
			OwnerID: uuid.MustParse(userA), Position: 0,
		},
		generated.CreateMilestoneParams{
			Number: project.Number, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("create-milestone-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	milestoneCreated, ok := milestoneResult.(*generated.MilestoneCreatedHeaders)
	require.True(t, ok, "unexpected Milestone response %T", milestoneResult)
	milestone := milestoneCreated.Response
	require.Equal(t, generated.MilestoneStatusPlanned, milestone.Status)
	require.Equal(t, `"1"`, milestoneCreated.Etag.Or(""))

	milestoneCriterionResult, err := client.CreateMilestoneCriterion(
		ctx,
		&generated.CriterionCreate{
			Criterion:                "The Milestone outcome is demonstrated",
			VerificationInstructions: "Run TestGeneratedClientAgentWorkflow",
			Position:                 0,
		},
		generated.CreateMilestoneCriterionParams{
			Number: project.Number, ID: milestone.ID,
			IfMatch: `"1"`, XProjectIfMatch: `"2"`,
			IdempotencyKey: generated.NewOptString("milestone-criterion-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	milestoneCriterionCreated, ok := milestoneCriterionResult.(*generated.CriterionCreatedHeaders)
	require.True(t, ok, "unexpected Milestone criterion response %T", milestoneCriterionResult)
	milestoneCriterion := milestoneCriterionCreated.Response

	activatedResult, err := client.ActivateMilestone(
		ctx,
		generated.ActivateMilestoneParams{
			Number: project.Number, ID: milestone.ID,
			IfMatch: `"2"`, XProjectIfMatch: `"3"`,
			IdempotencyKey: generated.NewOptString("activate-milestone-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	activated, ok := activatedResult.(*generated.MilestoneHeaders)
	require.True(t, ok, "unexpected activate response %T", activatedResult)
	require.Equal(t, generated.MilestoneStatusActive, activated.Response.Status)

	predecessorResult, err := client.CreateTask(
		ctx,
		&generated.TaskCreate{
			Title:          "Prepare the generated API fixture",
			Context:        "The relationship workflow needs an unfinished predecessor.",
			ExpectedResult: "The dependent task is blocked until this task concludes.",
			ProjectNumber:  project.Number,
			MilestoneID:    generated.NewOptNilUUID(milestone.ID),
		},
		generated.CreateTaskParams{
			IdempotencyKey: generated.NewOptString("create-predecessor-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	predecessorCreated, ok := predecessorResult.(*generated.TaskCreatedHeaders)
	require.True(t, ok, "unexpected predecessor response %T", predecessorResult)
	predecessor := predecessorCreated.Response
	cleanupTaskRow(t, db, predecessor.ID)

	taskResult, err := client.CreateTask(
		ctx,
		&generated.TaskCreate{
			Title:             "Exercise the generated API",
			Context:           "The generated API workflow needs end-to-end verification.",
			ExpectedResult:    "The Agent completes the versioned workflow through generated types.",
			ProjectNumber:     project.Number,
			MilestoneID:       generated.NewOptNilUUID(milestone.ID),
			DependencyNumbers: []int64{predecessor.Number},
		},
		generated.CreateTaskParams{
			IdempotencyKey: generated.NewOptString("create-task-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	taskCreated, ok := taskResult.(*generated.TaskCreatedHeaders)
	require.True(t, ok, "unexpected task response %T", taskResult)
	task := taskCreated.Response
	cleanupTaskRow(t, db, task.ID)
	require.Equal(t, project.Number, task.Project.Number)
	require.True(t, task.Blocked)
	require.Len(t, task.Dependencies, 1)
	require.Equal(t, predecessor.Number, task.Dependencies[0].Number)

	taskCriterionResult, err := client.CreateTaskCriterion(
		ctx,
		&generated.CriterionCreate{
			Criterion:                "The API workflow is executable by an Agent",
			VerificationInstructions: "Use the generated client to finish the task",
			Position:                 0,
		},
		generated.CreateTaskCriterionParams{
			Number: task.Number, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("task-criterion-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	taskCriterionCreated, ok := taskCriterionResult.(*generated.CriterionCreatedHeaders)
	require.True(t, ok, "unexpected task criterion response %T", taskCriterionResult)

	checkResult, err := client.CreateAcceptanceCheck(
		ctx,
		&generated.AcceptanceCheckCreate{
			CriterionRevision: milestoneCriterion.Revision,
			Outcome:           generated.AcceptanceOutcomePassed,
			Evidence:          "The generated Agent client verified this Milestone criterion.",
		},
		generated.CreateAcceptanceCheckParams{
			ID: milestoneCriterion.ID, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("check-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	createdCheck, ok := checkResult.(*generated.AcceptanceCheckCreatedHeaders)
	require.True(t, ok, "unexpected acceptance check response %T: %#v", checkResult, checkResult)
	require.Equal(t, generated.AcceptanceCheckCheckerTypeAgent, createdCheck.Response.CheckerType)
	require.Equal(t, issued.Name, createdCheck.Response.CheckerRef.Or(""))

	completeTask := func(number, initialVersion int64, criterion *generated.AcceptanceCriterion) generated.TaskWorkflow {
		readyResult, readyErr := client.MarkTaskReady(ctx, generated.MarkTaskReadyParams{
			Number: number, IfMatch: fmt.Sprintf(`"%d"`, initialVersion),
			IdempotencyKey: generated.NewOptString("ready-" + uuid.NewString()),
		})
		require.NoError(t, readyErr)
		ready, readyOK := readyResult.(*generated.TaskWorkflowHeaders)
		require.True(t, readyOK, "unexpected ready response %T", readyResult)

		executionSessionID := "execution-" + uuid.NewString()
		claimResult, claimErr := client.CreateTaskStageClaim(
			ctx,
			&generated.TaskStageClaimCreate{
				ClientKind:      generated.NewOptString("generated-client"),
				ClientSessionID: generated.NewOptString(executionSessionID),
			},
			generated.CreateTaskStageClaimParams{
				Number: number, IfMatch: fmt.Sprintf(`"%d"`, ready.Response.Version),
				IdempotencyKey: generated.NewOptString("claim-execution-" + uuid.NewString()),
			},
		)
		require.NoError(t, claimErr)
		execution, executionOK := claimResult.(*generated.TaskStageClaimCommandHeaders)
		require.True(t, executionOK, "unexpected execution Claim response %T", claimResult)
		currentResult, currentErr := client.GetCurrentTaskStageClaim(
			ctx,
			generated.GetCurrentTaskStageClaimParams{
				ClientKind: "generated-client", ClientSessionID: executionSessionID,
			},
		)
		require.NoError(t, currentErr)
		current, currentOK := currentResult.(*generated.TaskStageClaimHeaders)
		require.True(t, currentOK, "unexpected current Claim response %T", currentResult)
		require.Equal(t, execution.Response.Claim.ID, current.Response.ID)

		if criterion != nil {
			verificationResult, verificationErr := client.RecordTaskStageAcceptanceCheck(
				ctx,
				&generated.TaskStageAcceptanceCheckWrite{
					ClaimVersion:      execution.Response.Claim.Version,
					CriterionRevision: criterion.Revision,
					Outcome:           generated.AcceptanceOutcomePassed,
					Evidence:          "Execution verification passed.",
				},
				generated.RecordTaskStageAcceptanceCheckParams{
					Number: number, ID: execution.Response.Claim.ID, CriterionID: criterion.ID,
					IfMatch:        fmt.Sprintf(`"%d"`, execution.Response.Task.Version),
					IdempotencyKey: generated.NewOptString("verify-" + uuid.NewString()),
				},
			)
			require.NoError(t, verificationErr)
			verification, verificationOK := verificationResult.(*generated.AcceptanceCheckCreatedHeaders)
			require.True(t, verificationOK, "unexpected verification response %T", verificationResult)
			require.Equal(t, generated.AcceptanceCheckPurposeExecutionVerification, verification.Response.Purpose.Or(""))
		}

		submitResult, submitErr := client.RecordTaskWorkSubmission(
			ctx,
			&generated.TaskStageClaimFinish{ClaimVersion: execution.Response.Claim.Version, Body: "Work is ready for acceptance."},
			generated.RecordTaskWorkSubmissionParams{
				Number: number, ID: execution.Response.Claim.ID,
				IfMatch:        fmt.Sprintf(`"%d"`, execution.Response.Task.Version),
				IdempotencyKey: generated.NewOptString("submit-" + uuid.NewString()),
			},
		)
		require.NoError(t, submitErr)
		submitted, submittedOK := submitResult.(*generated.TaskWorkSubmissionCommandHeaders)
		require.True(t, submittedOK, "unexpected submit response %T", submitResult)
		require.Equal(t, generated.TaskPhaseInProgress, submitted.Response.Task.Phase)
		require.Equal(t, generated.TaskStageClaimStatusActive, submitted.Response.Claim.Status)

		completionResult, completionErr := client.CompleteTaskExecution(
			ctx,
			&generated.TaskStageClaimFinish{ClaimVersion: execution.Response.Claim.Version, Body: "Execution is complete."},
			generated.CompleteTaskExecutionParams{
				Number: number, ID: execution.Response.Claim.ID,
				IfMatch:        fmt.Sprintf(`"%d"`, execution.Response.Task.Version),
				IdempotencyKey: generated.NewOptString("complete-" + uuid.NewString()),
			},
		)
		require.NoError(t, completionErr)
		completed, completedOK := completionResult.(*generated.TaskExecutionCompletionCommandHeaders)
		require.True(t, completedOK, "unexpected completion response %T", completionResult)

		reviewClaimResult, reviewClaimErr := client.CreateTaskStageClaim(
			ctx,
			&generated.TaskStageClaimCreate{
				ClientKind:      generated.NewOptString("generated-client"),
				ClientSessionID: generated.NewOptString("review-" + uuid.NewString()),
			},
			generated.CreateTaskStageClaimParams{
				Number: number, IfMatch: fmt.Sprintf(`"%d"`, completed.Response.Task.Version),
				IdempotencyKey: generated.NewOptString("claim-review-" + uuid.NewString()),
			},
		)
		require.NoError(t, reviewClaimErr)
		review, reviewOK := reviewClaimResult.(*generated.TaskStageClaimCommandHeaders)
		require.True(t, reviewOK, "unexpected review Claim response %T", reviewClaimResult)

		if criterion != nil {
			acceptanceResult, acceptanceErr := client.RecordTaskStageAcceptanceCheck(
				ctx,
				&generated.TaskStageAcceptanceCheckWrite{
					ClaimVersion:      review.Response.Claim.Version,
					CriterionRevision: criterion.Revision,
					Outcome:           generated.AcceptanceOutcomePassed,
					Evidence:          "Acceptance review passed.",
				},
				generated.RecordTaskStageAcceptanceCheckParams{
					Number: number, ID: review.Response.Claim.ID, CriterionID: criterion.ID,
					IfMatch:        fmt.Sprintf(`"%d"`, review.Response.Task.Version),
					IdempotencyKey: generated.NewOptString("acceptance-" + uuid.NewString()),
				},
			)
			require.NoError(t, acceptanceErr)
			acceptance, acceptanceOK := acceptanceResult.(*generated.AcceptanceCheckCreatedHeaders)
			require.True(t, acceptanceOK, "unexpected acceptance response %T", acceptanceResult)
			require.Equal(t, generated.AcceptanceCheckPurposeAcceptance, acceptance.Response.Purpose.Or(""))
		}

		acceptResult, acceptErr := client.AcceptTask(
			ctx,
			&generated.TaskStageClaimFinish{ClaimVersion: review.Response.Claim.Version, Body: "Accepted."},
			generated.AcceptTaskParams{
				Number: number, ID: review.Response.Claim.ID,
				IfMatch:        fmt.Sprintf(`"%d"`, review.Response.Task.Version),
				IdempotencyKey: generated.NewOptString("accept-" + uuid.NewString()),
			},
		)
		require.NoError(t, acceptErr)
		accepted, acceptedOK := acceptResult.(*generated.TaskStageClaimCommandHeaders)
		require.True(t, acceptedOK, "unexpected accept response %T", acceptResult)
		return accepted.Response.Task
	}

	completedPredecessor := completeTask(predecessor.Number, predecessor.Version, nil)
	require.Equal(t, generated.TaskPhaseDone, completedPredecessor.Phase)

	completedTask := completeTask(task.Number, task.Version+1, &taskCriterionCreated.Response)
	require.Equal(t, generated.TaskPhaseDone, completedTask.Phase)

	completedMilestoneResult, err := client.CompleteMilestone(
		ctx, generated.OptLifecycleRequest{},
		generated.CompleteMilestoneParams{
			Number: project.Number, ID: milestone.ID,
			IfMatch: `"4"`, XProjectIfMatch: `"5"`,
			IdempotencyKey: generated.NewOptString("complete-milestone-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedMilestone, ok := completedMilestoneResult.(*generated.MilestoneHeaders)
	require.True(t, ok, "unexpected complete Milestone response %T", completedMilestoneResult)
	require.Equal(t, generated.MilestoneStatusCompleted, completedMilestone.Response.Status)

	archivedProjectResult, err := client.ArchiveProject(
		ctx,
		generated.ArchiveProjectParams{
			Number: project.Number, IfMatch: `"6"`,
			IdempotencyKey: generated.NewOptString("archive-project-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	archivedProject, ok := archivedProjectResult.(*generated.ProjectHeaders)
	require.True(t, ok, "unexpected archive Project response %T", archivedProjectResult)
	_, archived := archivedProject.Response.ArchivedAt.Get()
	require.True(t, archived)
}

func formatTestETag(version int64) string {
	return `"` + fmt.Sprint(version) + `"`
}
