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
		Name: "Generated Agent workflow", OwnerID: uuid.MustParse(userA),
	}
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

	for _, criterion := range []generated.AcceptanceCriterion{
		milestoneCriterion, taskCriterionCreated.Response,
	} {
		checkResult, err := client.CreateAcceptanceCheck(
			ctx,
			&generated.AcceptanceCheckCreate{
				CriterionRevision: criterion.Revision,
				Outcome:           generated.AcceptanceOutcomePassed,
				Evidence:          "The generated Agent client verified this criterion.",
			},
			generated.CreateAcceptanceCheckParams{
				ID: criterion.ID, IfMatch: `"1"`,
				IdempotencyKey: generated.NewOptString("check-" + uuid.NewString()),
			},
		)
		require.NoError(t, err)
		created, ok := checkResult.(*generated.AcceptanceCheckCreatedHeaders)
		require.True(t, ok, "unexpected acceptance check response %T", checkResult)
		require.Equal(t, generated.AcceptanceCheckCheckerTypeAgent, created.Response.CheckerType)
		require.Equal(t, issued.Name, created.Response.CheckerRef.Or(""))
	}

	completedPredecessorResult, err := client.UpdateTask(
		ctx,
		&generated.TaskPatch{Status: generated.NewOptTaskStatus(generated.TaskStatusDone)},
		generated.UpdateTaskParams{
			Number: predecessor.Number, IfMatch: `"1"`,
			IdempotencyKey: generated.NewOptString("complete-predecessor-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedPredecessor, ok := completedPredecessorResult.(*generated.TaskHeaders)
	require.True(t, ok, "unexpected predecessor completion response %T", completedPredecessorResult)
	require.Equal(t, generated.TaskStatusDone, completedPredecessor.Response.Status)

	completedTaskResult, err := client.UpdateTask(
		ctx,
		&generated.TaskPatch{Status: generated.NewOptTaskStatus(generated.TaskStatusDone)},
		generated.UpdateTaskParams{
			Number: task.Number, IfMatch: `"3"`,
			IdempotencyKey: generated.NewOptString("complete-task-" + uuid.NewString()),
		},
	)
	require.NoError(t, err)
	completedTask, ok := completedTaskResult.(*generated.TaskHeaders)
	require.True(t, ok, "unexpected complete task response %T", completedTaskResult)
	require.Equal(t, generated.TaskStatusDone, completedTask.Response.Status)

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
