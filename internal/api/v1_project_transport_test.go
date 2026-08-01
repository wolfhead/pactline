package api_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	baseapi "github.com/wolfhead/pactline/internal/api"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type v1ProjectJSON struct {
	ID      uuid.UUID `json:"id"`
	Number  int64     `json:"number"`
	Version int64     `json:"version"`
}

type v1ProjectDetailJSON struct {
	Project    v1ProjectJSON     `json:"project"`
	Milestones []v1MilestoneJSON `json:"milestones"`
}

type v1MilestoneJSON struct {
	ID                 uuid.UUID         `json:"id"`
	Version            int64             `json:"version"`
	Name               string            `json:"name"`
	Status             string            `json:"status"`
	OwnerID            uuid.UUID         `json:"owner_id"`
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

func TestV1ProjectMembershipControlsVisibilityAndAdministration(t *testing.T) {
	handler, db := newTaskTestServer(t)

	memberCreate := do(t, handler, http.MethodPost, "/api/v1/projects", userB, map[string]any{
		"name": "Member-created Project",
	})
	require.Equal(t, http.StatusCreated, memberCreate.Code, memberCreate.Body.String())
	var memberProject v1ProjectJSON
	decodeJSON(t, memberCreate, &memberProject)
	cleanupProjectRows(t, db, memberProject.ID)

	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "Project-scoped administration",
	})
	require.Equal(t, http.StatusCreated, createdProject.Code, createdProject.Body.String())
	var project v1ProjectJSON
	decodeJSON(t, createdProject, &project)
	cleanupProjectRows(t, db, project.ID)

	hidden := do(t, handler, http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%d", project.Number), userB, nil)
	require.Equal(t, http.StatusNotFound, hidden.Code, hidden.Body.String())

	addMember := doWithHeaders(
		t, handler, http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%d/members", project.Number), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"user_id": userB, "role": "member"},
	)
	require.Equal(t, http.StatusCreated, addMember.Code, addMember.Body.String())

	visible := do(t, handler, http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%d", project.Number), userB, nil)
	require.Equal(t, http.StatusOK, visible.Code, visible.Body.String())

	memberArchive := doWithHeaders(
		t,
		handler,
		http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%d/archive", project.Number),
		userB,
		http.Header{"If-Match": {`"2"`}},
		nil,
	)
	require.Equal(t, http.StatusForbidden, memberArchive.Code, memberArchive.Body.String())
}

func TestV1ProjectMilestoneAndAcceptanceVersions(t *testing.T) {
	handler, db := newTaskTestServer(t)

	createdProject := do(t, handler, http.MethodPost, "/api/v1/projects", userA, map[string]any{
		"name": "Versioned Project",
	})
	require.Equal(t, http.StatusCreated, createdProject.Code, createdProject.Body.String())
	require.Equal(t, `"1"`, createdProject.Header().Get("ETag"))
	var project v1ProjectJSON
	decodeJSON(t, createdProject, &project)
	cleanupProjectRows(t, db, project.ID)
	projectPath := "/api/v1/projects/" + strconv.FormatInt(project.Number, 10)

	createdMilestone := doWithHeaders(
		t, handler, http.MethodPost, projectPath+"/milestones", userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{
			"name": "Transport ready", "outcome": "The transport is verified",
			"owner_id": userA, "position": 0,
		},
	)
	require.Equal(t, http.StatusCreated, createdMilestone.Code, createdMilestone.Body.String())
	require.Equal(t, `"1"`, createdMilestone.Header().Get("ETag"))
	var milestone v1MilestoneJSON
	decodeJSON(t, createdMilestone, &milestone)
	require.Equal(t, "planned", milestone.Status)
	require.Equal(t, uuid.MustParse(userA), milestone.OwnerID)
	milestonePath := fmt.Sprintf("%s/milestones/%s", projectPath, milestone.ID)

	missingProjectPrecondition := doWithHeaders(
		t, handler, http.MethodPatch, milestonePath, userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"name": "Missing Project ETag"},
	)
	require.Equal(t, http.StatusPreconditionRequired, missingProjectPrecondition.Code)
	var missingProblem baseapi.Problem
	decodeJSON(t, missingProjectPrecondition, &missingProblem)
	require.Equal(t, "PRECONDITION_REQUIRED", missingProblem.Code)

	updatedMilestone := doWithHeaders(
		t, handler, http.MethodPatch, milestonePath, userA,
		http.Header{
			"If-Match":           {`"1"`},
			"X-Project-If-Match": {`"2"`},
		},
		map[string]any{"name": "Transport verified"},
	)
	require.Equal(t, http.StatusOK, updatedMilestone.Code, updatedMilestone.Body.String())
	require.Equal(t, `"2"`, updatedMilestone.Header().Get("ETag"))

	createdMilestoneCriterion := doWithHeaders(
		t, handler, http.MethodPost, milestonePath+"/criteria", userA,
		http.Header{
			"If-Match":           {`"2"`},
			"X-Project-If-Match": {`"3"`},
		},
		map[string]any{
			"criterion":                 "Milestone API is verified",
			"verification_instructions": "Run the Project API test",
			"position":                  0,
		},
	)
	require.Equal(
		t, http.StatusCreated, createdMilestoneCriterion.Code,
		createdMilestoneCriterion.Body.String(),
	)
	var milestoneCriterion v1CriterionJSON
	decodeJSON(t, createdMilestoneCriterion, &milestoneCriterion)

	missingActivationPrecondition := doWithHeaders(
		t, handler, http.MethodPost, milestonePath+"/activate", userA,
		http.Header{"If-Match": {`"3"`}}, nil,
	)
	require.Equal(t, http.StatusPreconditionRequired, missingActivationPrecondition.Code)
	decodeJSON(t, missingActivationPrecondition, &missingProblem)
	require.Equal(t, "PRECONDITION_REQUIRED", missingProblem.Code)

	activated := doWithHeaders(
		t, handler, http.MethodPost, milestonePath+"/activate", userA,
		http.Header{
			"If-Match":           {`"3"`},
			"X-Project-If-Match": {`"4"`},
		}, nil,
	)
	require.Equal(t, http.StatusOK, activated.Code, activated.Body.String())
	require.Equal(t, `"4"`, activated.Header().Get("ETag"))

	projectDetail := do(t, handler, http.MethodGet, projectPath, userA, nil)
	require.Equal(t, http.StatusOK, projectDetail.Code, projectDetail.Body.String())
	require.Equal(t, `"5"`, projectDetail.Header().Get("ETag"))
	var detail v1ProjectDetailJSON
	decodeJSON(t, projectDetail, &detail)
	require.Len(t, detail.Milestones, 1)
	require.Equal(t, "active", detail.Milestones[0].Status)
	require.Len(t, detail.Milestones[0].AcceptanceCriteria, 1)

	taskCreated := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Verify versioned criteria",
		"context":         "Versioned acceptance needs transport coverage",
		"expected_result": "Criterion changes preserve every aggregate version",
		"project_number":  project.Number,
		"milestone_id":    milestone.ID,
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
}
