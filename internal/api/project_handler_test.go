package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type projectResponse struct {
	ID                uuid.UUID `json:"id"`
	Number            int64     `json:"number"`
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	ActiveCriteria    int       `json:"active_criteria"`
	SatisfiedCriteria int       `json:"satisfied_criteria"`
}

type milestoneResponse struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

type criterionResponse struct {
	ID       uuid.UUID `json:"id"`
	Revision int       `json:"revision"`
}

func cleanupProjectRows(t *testing.T, db *store.DB, projectID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		statements := []string{
			`DELETE FROM acceptance_checks WHERE criterion_id IN (
				SELECT id FROM acceptance_criteria
				WHERE project_id=$1 OR milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
			)`,
			`DELETE FROM acceptance_criteria
			 WHERE project_id=$1 OR milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)`,
			`DELETE FROM project_activity WHERE project_id=$1`,
			`UPDATE tasks SET project_id=NULL, milestone_id=NULL WHERE project_id=$1`,
			`DELETE FROM milestones WHERE project_id=$1`,
			`DELETE FROM projects WHERE id=$1`,
		}
		for _, statement := range statements {
			_, err := db.Pool.Exec(ctx, statement, projectID)
			require.NoError(t, err)
		}
	})
}

func TestProjectMilestoneAcceptanceAndTaskAssociationWorkflow(t *testing.T) {
	h, db := newTaskTestServer(t)

	rec := do(t, h, http.MethodPost, "/api/projects", userA, map[string]any{
		"name": "Release project surface", "outcome": "Projects are usable",
		"owner_id": userA,
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var project projectResponse
	decodeJSON(t, rec, &project)
	cleanupProjectRows(t, db, project.ID)

	rec = do(t, h, http.MethodPost,
		fmt.Sprintf("/api/projects/%d/acceptance-criteria", project.Number), userA,
		map[string]any{
			"criterion":                 "Project workflow passes",
			"verification_instructions": "Run the project workflow test",
			"position":                  0,
		})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var projectCriterion criterionResponse
	decodeJSON(t, rec, &projectCriterion)

	rec = do(t, h, http.MethodPost, fmt.Sprintf("/api/projects/%d/activate", project.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, h, http.MethodPost, fmt.Sprintf("/api/projects/%d/milestones", project.Number), userA,
		map[string]any{
			"name": "API ready", "outcome": "The API workflow is verified", "position": 0,
		})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var milestone milestoneResponse
	decodeJSON(t, rec, &milestone)

	rec = do(t, h, http.MethodPost,
		fmt.Sprintf("/api/projects/%d/milestones/%s/acceptance-criteria", project.Number, milestone.ID),
		userA, map[string]any{
			"criterion": "API is ready", "verification_instructions": "Run API tests", "position": 0,
		})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var milestoneCriterion criterionResponse
	decodeJSON(t, rec, &milestoneCriterion)

	task := mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": "Verify API", "status": "done",
		"project_number": project.Number, "milestone_id": milestone.ID,
	})
	require.NotNil(t, task.Project)
	require.Equal(t, project.Number, task.Project.Number)
	require.NotNil(t, task.Milestone)
	require.Equal(t, milestone.ID, task.Milestone.ID)

	for _, criterion := range []criterionResponse{projectCriterion, milestoneCriterion} {
		rec = do(t, h, http.MethodPost,
			fmt.Sprintf("/api/acceptance-criteria/%s/checks", criterion.ID), userA,
			map[string]any{
				"criterion_revision": criterion.Revision,
				"outcome":            "passed", "evidence": "Test output is green",
			})
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost,
		fmt.Sprintf("/api/projects/%d/milestones/%s/complete", project.Number, milestone.ID), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, h, http.MethodPost, fmt.Sprintf("/api/projects/%d/complete", project.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &project)
	require.Equal(t, "completed", project.Status)

	rec = do(t, h, http.MethodPost, "/api/tasks", userA, map[string]any{
		"title": "Must not enter a completed project", "project_number": project.Number,
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}
