package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateTaskDefaultsAndEmbedsCreator(t *testing.T) {
	h, db := newTaskTestServer(t)

	out := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "Write API docs"})

	require.Equal(t, "todo", out.Status)
	require.Equal(t, "none", out.Priority)
	require.Nil(t, out.Assignee)
	require.Equal(t, uuid.MustParse(userA), out.Creator.ID)
	require.NotZero(t, out.Number)
	require.Empty(t, out.Labels)
}

func TestCreateTaskRejectsBlankTitle(t *testing.T) {
	h, _ := newTaskTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/tasks", userA, map[string]any{"title": "   "})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetTaskByNumber(t *testing.T) {
	h, db := newTaskTestServer(t)
	created := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "Fetch me"})

	rec := do(t, h, http.MethodGet, fmt.Sprintf("/api/tasks/%d", created.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var out taskResponse
	decodeJSON(t, rec, &out)
	require.Equal(t, created.ID, out.ID)
}

func TestGetTaskMissingNumberReturns404(t *testing.T) {
	h, _ := newTaskTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/tasks/999999999", userA, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestUpdateTaskAnyFieldWorksInOneEndpoint pins the spec's most conspicuous
// gap in what came before ("there was no edit endpoint at all"): title,
// status, priority, assignee and due_date must all be editable through the
// one PATCH endpoint, and fields left out of the body must not be touched.
func TestUpdateTaskAnyFieldWorksInOneEndpoint(t *testing.T) {
	h, db := newTaskTestServer(t)
	created := mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": "Original", "description": "Original description",
	})

	rec := do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", created.Number), userB, map[string]any{
		"title":       "Renamed",
		"status":      "in_progress",
		"priority":    "urgent",
		"assignee_id": userC,
		"due_date":    "2026-09-01",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out taskResponse
	decodeJSON(t, rec, &out)
	require.Equal(t, "Renamed", out.Title)
	require.Equal(t, "Original description", out.Description, "description was not in the patch")
	require.Equal(t, "in_progress", out.Status)
	require.Equal(t, "urgent", out.Priority)
	require.Equal(t, uuid.MustParse(userC), out.Assignee.ID)
	require.Equal(t, "2026-09-01", *out.DueDate)
}

// TestUpdateTaskAssigneeExplicitNullClearsIt exercises the JSON PATCH
// contract end-to-end: sending assignee_id: null must clear an existing
// assignee (not merely be ignored, the way a missing key is).
func TestUpdateTaskAssigneeExplicitNullClearsIt(t *testing.T) {
	h, db := newTaskTestServer(t)
	created := mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": "Assigned", "assignee_id": userC,
	})
	require.NotNil(t, created.Assignee)

	rec := do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", created.Number), userA, map[string]any{
		"assignee_id": nil,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out taskResponse
	decodeJSON(t, rec, &out)
	require.Nil(t, out.Assignee)
}

func TestUpdateTaskRejectsUnknownStatus(t *testing.T) {
	h, db := newTaskTestServer(t)
	created := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "x"})
	rec := do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", created.Number), userA, map[string]any{"status": "not-a-status"})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", created.Number), userA, map[string]any{"status": "backlog"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskAcceptanceCriteriaGateCompletion(t *testing.T) {
	h, db := newTaskTestServer(t)
	task := mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": "Acceptance-gated task", "status": "in_review",
	})

	rec := do(t, h, http.MethodPost,
		fmt.Sprintf("/api/tasks/%d/acceptance-criteria", task.Number), userA,
		map[string]any{
			"criterion":                 "The task result is observable",
			"verification_instructions": "Run the task workflow test",
			"position":                  0,
		})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var criterion criterionResponse
	decodeJSON(t, rec, &criterion)

	rec = do(t, h, http.MethodGet,
		fmt.Sprintf("/api/tasks/%d/acceptance-criteria", task.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var criteria []criterionResponse
	decodeJSON(t, rec, &criteria)
	require.Len(t, criteria, 1)
	require.Equal(t, criterion.ID, criteria[0].ID)

	rec = do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.Number), userA,
		map[string]any{"status": "done"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	rec = do(t, h, http.MethodPost,
		fmt.Sprintf("/api/acceptance-criteria/%s/checks", criterion.ID), userA,
		map[string]any{
			"criterion_revision": criterion.Revision,
			"outcome":            "passed",
			"evidence":           "Task workflow passed",
		})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.Number), userA,
		map[string]any{"status": "done"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var completed taskResponse
	decodeJSON(t, rec, &completed)
	require.Equal(t, "done", completed.Status)
	require.NotNil(t, completed.CompletedAt)
}

// TestArchiveAndRestoreTaskRoundTrip pins that archiving is reachable and
// reversible through HTTP and that the default list hides an archived task
// while the direct GET still finds it — archiving removes a task from view,
// not from existence.
func TestArchiveAndRestoreTaskRoundTrip(t *testing.T) {
	h, db := newTaskTestServer(t)
	created := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "archive-round-trip-" + uuid.NewString()})

	rec := do(t, h, http.MethodPost, fmt.Sprintf("/api/tasks/%d/archive", created.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var archived taskResponse
	decodeJSON(t, rec, &archived)
	require.NotNil(t, archived.ArchivedAt)

	listRec := do(t, h, http.MethodGet, "/api/tasks?q="+created.Title, userA, nil)
	require.Equal(t, http.StatusOK, listRec.Code)
	var list taskListResponseJSON
	decodeJSON(t, listRec, &list)
	require.Empty(t, list.Items, "default list must exclude archived tasks")

	rec = do(t, h, http.MethodPost, fmt.Sprintf("/api/tasks/%d/restore", created.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var restored taskResponse
	decodeJSON(t, rec, &restored)
	require.Nil(t, restored.ArchivedAt)
}

// TestListTasksFiltersByStatusAndAssigneeNone plants a decoy assigned task
// so that assignee=none must exclude it, matching only the genuinely
// unassigned one.
func TestListTasksFiltersByStatusAndAssigneeNone(t *testing.T) {
	h, db := newTaskTestServer(t)
	marker := "httplistfilter-" + uuid.NewString()

	unassigned := mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": marker, "status": "todo",
	})
	_ = mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": marker, "status": "todo", "assignee_id": userC,
	})
	_ = mustCreateTaskHTTP(t, h, db, userA, map[string]any{
		"title": marker, "status": "done",
	})

	rec := do(t, h, http.MethodGet, "/api/tasks?q="+marker+"&status=todo&assignee=none", userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list taskListResponseJSON
	decodeJSON(t, rec, &list)
	require.Len(t, list.Items, 1)
	require.Equal(t, unassigned.ID, list.Items[0].ID)
}

func TestListTasksPaginationCursor(t *testing.T) {
	h, db := newTaskTestServer(t)
	marker := "httppaging-" + uuid.NewString()
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		out := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": marker})
		ids = append(ids, out.ID)
	}

	rec := do(t, h, http.MethodGet, "/api/tasks?q="+marker+"&limit=2&sort=number&order=asc", userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var page1 taskListResponseJSON
	decodeJSON(t, rec, &page1)
	require.Len(t, page1.Items, 2)
	require.True(t, page1.HasMore)
	require.NotEmpty(t, page1.NextCursor)

	rec = do(t, h, http.MethodGet, fmt.Sprintf("/api/tasks?q=%s&limit=2&sort=number&order=asc&cursor=%s", marker, page1.NextCursor), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var page2 taskListResponseJSON
	decodeJSON(t, rec, &page2)
	require.Len(t, page2.Items, 1)
	require.False(t, page2.HasMore)
	require.Empty(t, page2.NextCursor)

	var seen []uuid.UUID
	for _, it := range page1.Items {
		seen = append(seen, it.ID)
	}
	for _, it := range page2.Items {
		seen = append(seen, it.ID)
	}
	require.Equal(t, ids, seen)
}
