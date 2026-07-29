package api_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	baseapi "bountyboard/internal/api"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type v1ProjectJSON struct {
	ID      uuid.UUID `json:"id"`
	Number  int64     `json:"number"`
	Version int64     `json:"version"`
	Status  string    `json:"status"`
}

type v1ProjectDetailJSON struct {
	Project            v1ProjectJSON     `json:"project"`
	AcceptanceCriteria []v1CriterionJSON `json:"acceptance_criteria"`
	Milestones         []v1MilestoneJSON `json:"milestones"`
}

type v1MilestoneJSON struct {
	ID                 uuid.UUID         `json:"id"`
	Version            int64             `json:"version"`
	Name               string            `json:"name"`
	AcceptanceCriteria []v1CriterionJSON `json:"acceptance_criteria"`
}

type v1CriterionJSON struct {
	ID           uuid.UUID              `json:"id"`
	Version      int64                  `json:"version"`
	Revision     int                    `json:"revision"`
	CurrentCheck *v1AcceptanceCheckJSON `json:"current_check"`
}

type v1AcceptanceCheckJSON struct {
	ID          uuid.UUID `json:"id"`
	Outcome     string    `json:"outcome"`
	CheckerType string    `json:"checker_type"`
}

func TestV1ProjectMilestoneAndAcceptanceVersions(t *testing.T) {
	handler, db := newTaskTestServer(t)

	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "Versioned project", "outcome": "Aggregate mutations are safe",
		"owner_id": userA,
	})
	require.Equal(t, http.StatusCreated, createdProject.Code, createdProject.Body.String())
	require.Equal(t, `"1"`, createdProject.Header().Get("ETag"))
	var project v1ProjectJSON
	decodeJSON(t, createdProject, &project)
	cleanupProjectRows(t, db, project.ID)
	projectPath := "/api/v1/projects/" + strconv.FormatInt(project.Number, 10)

	createdProjectCriterion := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/criteria", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion":                 "The aggregate workflow passes",
			"verification_instructions": "Run the versioned project transport test",
			"position":                  0,
		},
	)
	require.Equal(
		t, http.StatusCreated, createdProjectCriterion.Code,
		createdProjectCriterion.Body.String(),
	)
	require.Equal(t, `"1"`, createdProjectCriterion.Header().Get("ETag"))
	var projectCriterion v1CriterionJSON
	decodeJSON(t, createdProjectCriterion, &projectCriterion)

	projectAfterCriterion := do(t, handler, http.MethodGet, projectPath, userA, nil)
	require.Equal(t, http.StatusOK, projectAfterCriterion.Code, projectAfterCriterion.Body.String())
	require.Equal(t, `"2"`, projectAfterCriterion.Header().Get("ETag"))

	staleCriterionCreate := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/criteria", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion":                 "Stale criterion",
			"verification_instructions": "This must not be created",
			"position":                  1,
		},
	)
	assertVersionConflict(t, staleCriterionCreate, 2)

	activated := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/activate", userA,
		http.Header{"If-Match": {`"2"`}}, nil,
	)
	require.Equal(t, http.StatusOK, activated.Code, activated.Body.String())
	require.Equal(t, `"3"`, activated.Header().Get("ETag"))

	createdMilestone := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/milestones", userA,
		http.Header{"If-Match": {`"3"`}},
		map[string]any{
			"name": "Transport ready", "outcome": "The transport is verified", "position": 0,
		},
	)
	require.Equal(t, http.StatusCreated, createdMilestone.Code, createdMilestone.Body.String())
	require.Equal(t, `"1"`, createdMilestone.Header().Get("ETag"))
	var milestone v1MilestoneJSON
	decodeJSON(t, createdMilestone, &milestone)
	milestonePath := fmt.Sprintf("%s/milestones/%s", projectPath, milestone.ID)

	missingProjectPrecondition := doWithHeaders(
		t, handler, http.MethodPatch, milestonePath, userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"name": "Missing project ETag"},
	)
	require.Equal(t, http.StatusPreconditionRequired, missingProjectPrecondition.Code)
	var missingProblem baseapi.Problem
	decodeJSON(t, missingProjectPrecondition, &missingProblem)
	require.Equal(t, "PRECONDITION_REQUIRED", missingProblem.Code)

	updatedMilestone := doWithHeaders(
		t, handler, http.MethodPatch, milestonePath, userA,
		http.Header{
			"If-Match":           {`"1"`},
			"X-Project-If-Match": {`"4"`},
		},
		map[string]any{"name": "Transport verified"},
	)
	require.Equal(t, http.StatusOK, updatedMilestone.Code, updatedMilestone.Body.String())
	require.Equal(t, `"2"`, updatedMilestone.Header().Get("ETag"))

	createdMilestoneCriterion := doWithHeaders(
		t, handler, http.MethodPost, milestonePath+"/criteria", userA,
		http.Header{
			"If-Match":           {`"2"`},
			"X-Project-If-Match": {`"5"`},
		},
		map[string]any{
			"criterion":                 "Milestone API is verified",
			"verification_instructions": "Run the project API test",
			"position":                  0,
		},
	)
	require.Equal(
		t, http.StatusCreated, createdMilestoneCriterion.Code,
		createdMilestoneCriterion.Body.String(),
	)
	var milestoneCriterion v1CriterionJSON
	decodeJSON(t, createdMilestoneCriterion, &milestoneCriterion)

	projectDetail := do(t, handler, http.MethodGet, projectPath, userA, nil)
	require.Equal(t, http.StatusOK, projectDetail.Code, projectDetail.Body.String())
	require.Equal(t, `"6"`, projectDetail.Header().Get("ETag"))
	var detail v1ProjectDetailJSON
	decodeJSON(t, projectDetail, &detail)
	require.Len(t, detail.AcceptanceCriteria, 1)
	require.Len(t, detail.Milestones, 1)
	require.Equal(t, int64(3), detail.Milestones[0].Version)
	require.Len(t, detail.Milestones[0].AcceptanceCriteria, 1)

	taskCreated := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title": "Verify versioned criteria",
	})
	require.Equal(t, http.StatusCreated, taskCreated.Code, taskCreated.Body.String())
	var task v1TaskJSON
	decodeJSON(t, taskCreated, &task)
	cleanupTaskRow(t, db, task.ID)
	taskPath := "/api/v1/tasks/" + strconv.FormatInt(task.Number, 10)

	createdTaskCriterion := doWithHeaders(
		t, handler, http.MethodPost, taskPath+"/criteria", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion":                 "Task behavior is verified",
			"verification_instructions": "Record a passing check",
			"position":                  0,
		},
	)
	require.Equal(t, http.StatusCreated, createdTaskCriterion.Code, createdTaskCriterion.Body.String())
	var taskCriterion v1CriterionJSON
	decodeJSON(t, createdTaskCriterion, &taskCriterion)

	taskAfterCriterion := do(t, handler, http.MethodGet, taskPath, userA, nil)
	require.Equal(t, `"2"`, taskAfterCriterion.Header().Get("ETag"))

	createdCheck := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/criteria/"+taskCriterion.ID.String()+"/checks", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"criterion_revision": 1, "outcome": "passed",
			"evidence": "The transport test passed.",
		},
	)
	require.Equal(t, http.StatusCreated, createdCheck.Code, createdCheck.Body.String())
	var check v1AcceptanceCheckJSON
	decodeJSON(t, createdCheck, &check)
	require.Equal(t, "passed", check.Outcome)
	require.Equal(t, "user", check.CheckerType)

	taskAfterCheck := do(t, handler, http.MethodGet, taskPath, userA, nil)
	require.Equal(t, `"3"`, taskAfterCheck.Header().Get("ETag"))

	criteriaList := do(t, handler, http.MethodGet, taskPath+"/criteria", userA, nil)
	require.Equal(t, http.StatusOK, criteriaList.Code, criteriaList.Body.String())
	var criteria struct {
		Items []v1CriterionJSON `json:"items"`
	}
	decodeJSON(t, criteriaList, &criteria)
	require.Len(t, criteria.Items, 1)
	require.Equal(t, int64(2), criteria.Items[0].Version)
	require.NotNil(t, criteria.Items[0].CurrentCheck)
	require.Equal(t, check.ID, criteria.Items[0].CurrentCheck.ID)

	updatedCriterion := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/criteria/"+taskCriterion.ID.String(), userA,
		http.Header{"If-Match": {`"2"`}},
		map[string]any{"position": 2},
	)
	require.Equal(t, http.StatusOK, updatedCriterion.Code, updatedCriterion.Body.String())
	decodeJSON(t, updatedCriterion, &taskCriterion)
	require.Equal(t, int64(3), taskCriterion.Version)
	require.Equal(t, 1, taskCriterion.Revision)

	textUpdatedCriterion := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/criteria/"+taskCriterion.ID.String(), userA,
		http.Header{"If-Match": {`"3"`}},
		map[string]any{"criterion": "Task behavior and evidence are verified"},
	)
	require.Equal(
		t, http.StatusOK, textUpdatedCriterion.Code, textUpdatedCriterion.Body.String(),
	)
	decodeJSON(t, textUpdatedCriterion, &taskCriterion)
	require.Equal(t, int64(4), taskCriterion.Version)
	require.Equal(t, 2, taskCriterion.Revision)
}
