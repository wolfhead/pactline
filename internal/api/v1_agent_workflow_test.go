package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	generated "bountyboard/internal/api/v1generated"

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
		"scopes": []string{"work:write"}, "expires_in_days": 30,
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

	projectRequest := &generated.ProjectCreate{
		Name: "Generated Agent workflow", Outcome: "The Agent can manage a complete project",
		OwnerID: uuid.MustParse(userA),
	}
	projectKey := generated.NewOptString("create-project-" + uuid.NewString())
	projectResult, err := client.CreateProject(
		ctx, projectRequest,
		generated.CreateProjectParams{IdempotencyKey: projectKey},
	)
	require.NoError(t, err)
	projectCreated, ok := projectResult.(*generated.ProjectCreatedHeaders)
	require.True(t, ok, "unexpected create project response %T", projectResult)
	require.Equal(t, `"1"`, projectCreated.Etag.Or(""))
	project := projectCreated.Response
	cleanupProjectRows(t, db, project.ID)

	projectReplayResult, err := client.CreateProject(
		ctx, projectRequest,
		generated.CreateProjectParams{IdempotencyKey: projectKey},
	)
	require.NoError(t, err)
	projectReplay, ok := projectReplayResult.(*generated.ProjectCreatedHeaders)
	require.True(t, ok, "unexpected project replay response %T", projectReplayResult)
	require.Equal(t, project.ID, projectReplay.Response.ID)
	require.True(t, projectReplay.IdempotencyReplayed.Or(false))

	reusedResult, err := client.CreateProject(
		ctx, &generated.ProjectCreate{
			Name: "Different project", Outcome: projectRequest.Outcome,
			OwnerID: projectRequest.OwnerID,
		},
		generated.CreateProjectParams{IdempotencyKey: projectKey},
	)
	require.NoError(t, err)
	reusedProblem, ok := reusedResult.(*generated.ProblemStatusCodeWithHeaders)
	require.True(t, ok, "unexpected reused-key response %T", reusedResult)
	require.Equal(t, http.StatusConflict, reusedProblem.StatusCode)
	require.Equal(t, generated.ProblemCode("IDEMPOTENCY_KEY_REUSED"), reusedProblem.Response.Code)

	projectCriterionResult, err := client.CreateProjectCriterion(
		ctx,
		&generated.CriterionCreate{
			Criterion:                "The generated Agent workflow completes",
			VerificationInstructions: "Run TestGeneratedClientAgentWorkflow",
			Position:                 0,
		},
		generated.CreateProjectCriterionParams{
			Number: project.Number, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("project-criterion-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	projectCriterionCreated, ok := projectCriterionResult.(*generated.CriterionCreatedHeaders)
	require.True(t, ok, "unexpected project criterion response %T", projectCriterionResult)
	projectCriterion := projectCriterionCreated.Response

	staleActivationResult, err := client.ActivateProject(
		ctx, generated.OptLifecycleRequest{},
		generated.ActivateProjectParams{
			Number: project.Number, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("stale-activate-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	staleActivation, ok := staleActivationResult.(*generated.ProblemStatusCodeWithHeaders)
	require.True(t, ok, "unexpected stale activation response %T", staleActivationResult)
	require.Equal(t, http.StatusPreconditionFailed, staleActivation.StatusCode)
	require.Equal(t, generated.ProblemCode("VERSION_CONFLICT"), staleActivation.Response.Code)
	currentVersion, ok := staleActivation.Response.CurrentVersion.Get()
	require.True(t, ok)
	require.Equal(t, int64(2), currentVersion)

	projectCheckResult, err := client.CreateAcceptanceCheck(
		ctx,
		&generated.AcceptanceCheckCreate{
			CriterionRevision: 1, Outcome: generated.AcceptanceOutcomePassed,
			Evidence: "The generated client reached this verified step.",
		},
		generated.CreateAcceptanceCheckParams{
			ID: projectCriterion.ID, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("project-check-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	projectCheck, ok := projectCheckResult.(*generated.AcceptanceCheckCreatedHeaders)
	require.True(t, ok, "unexpected project check response %T", projectCheckResult)
	require.Equal(t, generated.AcceptanceCheckCheckerTypeAgent, projectCheck.Response.CheckerType)
	require.Equal(t, issued.Name, projectCheck.Response.CheckerRef.Or(""))

	projectDetailResult, err := client.GetProject(
		ctx, generated.GetProjectParams{Number: project.Number},
	)
	require.NoError(t, err)
	projectDetail, ok := projectDetailResult.(*generated.ProjectDetailHeaders)
	require.True(t, ok, "unexpected project detail response %T", projectDetailResult)
	require.Equal(t, `"3"`, projectDetail.Etag.Or(""))

	activatedResult, err := client.ActivateProject(
		ctx, generated.OptLifecycleRequest{},
		generated.ActivateProjectParams{
			Number: project.Number, IfMatch: projectDetail.Etag.Or(""),
			IdempotencyKey: generated.NewOptString("activate-project-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	activated, ok := activatedResult.(*generated.ProjectHeaders)
	require.True(t, ok, "unexpected activate response %T", activatedResult)
	require.Equal(t, generated.ProjectStatusActive, activated.Response.Status)
	require.Equal(t, `"4"`, activated.Etag.Or(""))

	milestoneResult, err := client.CreateMilestone(
		ctx,
		&generated.MilestoneCreate{
			Name: "Agent workflow verified", Outcome: "All work is checked", Position: 0,
		},
		generated.CreateMilestoneParams{
			Number: project.Number, IfMatch: activated.Etag.Or(""),
			IdempotencyKey: generated.NewOptString("create-milestone-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	milestoneCreated, ok := milestoneResult.(*generated.MilestoneCreatedHeaders)
	require.True(t, ok, "unexpected milestone response %T", milestoneResult)
	milestone := milestoneCreated.Response
	require.Equal(t, `"1"`, milestoneCreated.Etag.Or(""))

	projectAfterMilestoneResult, err := client.GetProject(
		ctx, generated.GetProjectParams{Number: project.Number},
	)
	require.NoError(t, err)
	projectAfterMilestone := projectAfterMilestoneResult.(*generated.ProjectDetailHeaders)
	require.Equal(t, `"5"`, projectAfterMilestone.Etag.Or(""))

	milestoneCriterionResult, err := client.CreateMilestoneCriterion(
		ctx,
		&generated.CriterionCreate{
			Criterion:                "The milestone outcome is demonstrated",
			VerificationInstructions: "Verify the generated workflow evidence",
			Position:                 0,
		},
		generated.CreateMilestoneCriterionParams{
			Number: project.Number, ID: milestone.ID,
			IfMatch: `"1"`, XProjectIfMatch: projectAfterMilestone.Etag.Or(""),
			IdempotencyKey: generated.NewOptString("milestone-criterion-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	milestoneCriterionCreated, ok := milestoneCriterionResult.(*generated.CriterionCreatedHeaders)
	require.True(t, ok, "unexpected milestone criterion response %T", milestoneCriterionResult)
	milestoneCriterion := milestoneCriterionCreated.Response

	milestoneCheckResult, err := client.CreateAcceptanceCheck(
		ctx,
		&generated.AcceptanceCheckCreate{
			CriterionRevision: 1, Outcome: generated.AcceptanceOutcomePassed,
			Evidence: "Milestone verification passed.",
		},
		generated.CreateAcceptanceCheckParams{
			ID: milestoneCriterion.ID, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("milestone-check-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	_, ok = milestoneCheckResult.(*generated.AcceptanceCheckCreatedHeaders)
	require.True(t, ok, "unexpected milestone check response %T", milestoneCheckResult)

	projectAfterMilestoneCheckResult, err := client.GetProject(
		ctx, generated.GetProjectParams{Number: project.Number},
	)
	require.NoError(t, err)
	projectAfterMilestoneCheck := projectAfterMilestoneCheckResult.(*generated.ProjectDetailHeaders)
	require.Equal(t, `"7"`, projectAfterMilestoneCheck.Etag.Or(""))
	require.Len(t, projectAfterMilestoneCheck.Response.Milestones, 1)
	milestoneETag := formatTestETag(
		projectAfterMilestoneCheck.Response.Milestones[0].Version,
	)
	require.Equal(t, `"3"`, milestoneETag)

	taskRequest := &generated.TaskCreate{
		Title:         "Exercise the generated API",
		ProjectNumber: generated.NewOptInt64(project.Number),
		MilestoneID:   generated.NewOptNilUUID(milestone.ID),
	}
	taskKey := generated.NewOptString("create-task-" + uuid.NewString())
	taskResult, err := client.CreateTask(
		ctx, taskRequest, generated.CreateTaskParams{IdempotencyKey: taskKey},
	)
	require.NoError(t, err)
	taskCreated, ok := taskResult.(*generated.TaskCreatedHeaders)
	require.True(t, ok, "unexpected task response %T", taskResult)
	task := taskCreated.Response
	cleanupTaskRow(t, db, task.ID)
	require.Equal(t, `"1"`, taskCreated.Etag.Or(""))

	taskReplayResult, err := client.CreateTask(
		ctx, taskRequest, generated.CreateTaskParams{IdempotencyKey: taskKey},
	)
	require.NoError(t, err)
	taskReplay, ok := taskReplayResult.(*generated.TaskCreatedHeaders)
	require.True(t, ok, "unexpected task replay response %T", taskReplayResult)
	require.Equal(t, task.ID, taskReplay.Response.ID)
	require.True(t, taskReplay.IdempotencyReplayed.Or(false))

	taskCriterionResult, err := client.CreateTaskCriterion(
		ctx,
		&generated.CriterionCreate{
			Criterion:                "The API workflow is executable by an Agent",
			VerificationInstructions: "Use the generated client to finish the task",
			Position:                 0,
		},
		generated.CreateTaskCriterionParams{
			Number: task.Number, IfMatch: taskCreated.Etag.Or(""),
			IdempotencyKey: generated.NewOptString("task-criterion-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	taskCriterionCreated, ok := taskCriterionResult.(*generated.CriterionCreatedHeaders)
	require.True(t, ok, "unexpected task criterion response %T", taskCriterionResult)
	taskCriterion := taskCriterionCreated.Response

	taskCheckResult, err := client.CreateAcceptanceCheck(
		ctx,
		&generated.AcceptanceCheckCreate{
			CriterionRevision: 1, Outcome: generated.AcceptanceOutcomePassed,
			Evidence: "The Agent completed the required API operations.",
		},
		generated.CreateAcceptanceCheckParams{
			ID: taskCriterion.ID, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("task-check-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	_, ok = taskCheckResult.(*generated.AcceptanceCheckCreatedHeaders)
	require.True(t, ok, "unexpected task check response %T", taskCheckResult)

	commentResult, err := client.CreateTaskComment(
		ctx, &generated.CommentWrite{Body: "Evidence recorded by the generated Agent client."},
		generated.CreateTaskCommentParams{
			Number: task.Number, IfMatch: `"3"`,
			IdempotencyKey: generated.NewOptString("task-comment-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	commentCreated, ok := commentResult.(*generated.CommentCreatedHeaders)
	require.True(t, ok, "unexpected comment response %T", commentResult)
	require.Equal(t, `"1"`, commentCreated.Etag.Or(""))

	staleTaskResult, err := client.UpdateTask(
		ctx,
		&generated.TaskPatch{
			Status: generated.NewOptTaskStatus(generated.TaskStatusDone),
		},
		generated.UpdateTaskParams{
			Number: task.Number, IfMatch: `"3"`,
			IdempotencyKey: generated.NewOptString("stale-task-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	staleTask, ok := staleTaskResult.(*generated.ProblemStatusCodeWithHeaders)
	require.True(t, ok, "unexpected stale task response %T", staleTaskResult)
	require.Equal(t, generated.ProblemCode("VERSION_CONFLICT"), staleTask.Response.Code)
	staleTaskVersion, ok := staleTask.Response.CurrentVersion.Get()
	require.True(t, ok)
	require.Equal(t, int64(4), staleTaskVersion)

	completedTaskResult, err := client.UpdateTask(
		ctx,
		&generated.TaskPatch{
			Status: generated.NewOptTaskStatus(generated.TaskStatusDone),
		},
		generated.UpdateTaskParams{
			Number: task.Number, IfMatch: `"4"`,
			IdempotencyKey: generated.NewOptString("complete-task-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedTask, ok := completedTaskResult.(*generated.TaskHeaders)
	require.True(t, ok, "unexpected complete task response %T", completedTaskResult)
	require.Equal(t, generated.TaskStatusDone, completedTask.Response.Status)
	require.Equal(t, `"5"`, completedTask.Etag.Or(""))

	completedMilestoneResult, err := client.CompleteMilestone(
		ctx, generated.OptLifecycleRequest{},
		generated.CompleteMilestoneParams{
			Number: project.Number, ID: milestone.ID,
			IfMatch: milestoneETag, XProjectIfMatch: projectAfterMilestoneCheck.Etag.Or(""),
			IdempotencyKey: generated.NewOptString("complete-milestone-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedMilestone, ok := completedMilestoneResult.(*generated.MilestoneHeaders)
	require.True(t, ok, "unexpected complete milestone response %T", completedMilestoneResult)
	require.Equal(t, generated.MilestoneStatusCompleted, completedMilestone.Response.Status)
	require.Equal(t, `"4"`, completedMilestone.Etag.Or(""))

	completedProjectResult, err := client.CompleteProject(
		ctx, generated.OptLifecycleRequest{},
		generated.CompleteProjectParams{
			Number: project.Number, IfMatch: `"8"`,
			IdempotencyKey: generated.NewOptString("complete-project-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedProject, ok := completedProjectResult.(*generated.ProjectHeaders)
	require.True(t, ok, "unexpected complete project response %T", completedProjectResult)
	require.Equal(t, generated.ProjectStatusCompleted, completedProject.Response.Status)
	require.Equal(t, `"9"`, completedProject.Etag.Or(""))
}

func formatTestETag(version int64) string {
	return `"` + fmt.Sprint(version) + `"`
}
