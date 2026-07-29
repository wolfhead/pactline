package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	baseapi "bountyboard/internal/api"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type v1TaskJSON struct {
	ID      uuid.UUID `json:"id"`
	Number  int64     `json:"number"`
	Version int64     `json:"version"`
	Title   string    `json:"title"`
	Labels  []struct {
		ID uuid.UUID `json:"id"`
	} `json:"labels"`
}

type v1CommentJSON struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
	Body    string    `json:"body"`
}

type v1LabelJSON struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
	Name    string    `json:"name"`
}

func TestV1TaskCommentAndLabelVersions(t *testing.T) {
	handler, db := newTaskTestServer(t)

	labelCreated := do(t, handler, http.MethodPost, "/api/v1/labels", userA, map[string]any{
		"name": "v1-version-" + uuid.NewString(),
	})
	require.Equal(t, http.StatusCreated, labelCreated.Code, labelCreated.Body.String())
	require.Equal(t, `"1"`, labelCreated.Header().Get("ETag"))
	var label v1LabelJSON
	decodeJSON(t, labelCreated, &label)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM labels WHERE id=$1`, label.ID)
		require.NoError(t, err)
	})

	labelUpdated := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/labels/"+label.ID.String(), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"name": "v1-renamed-" + uuid.NewString()},
	)
	require.Equal(t, http.StatusOK, labelUpdated.Code, labelUpdated.Body.String())
	require.Equal(t, `"2"`, labelUpdated.Header().Get("ETag"))
	decodeJSON(t, labelUpdated, &label)
	require.Equal(t, int64(2), label.Version)

	staleLabel := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/labels/"+label.ID.String(), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"name": "stale"},
	)
	assertVersionConflict(t, staleLabel, 2)

	taskCreated := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title":           "Versioned task",
		"context":         "Versioned task writes need transport coverage",
		"expected_result": "Task and related resource versions remain consistent",
		"label_ids":       []uuid.UUID{label.ID},
		"project_number":  activeProjectNumber(t, db),
	})
	require.Equal(t, http.StatusCreated, taskCreated.Code, taskCreated.Body.String())
	require.Equal(t, `"1"`, taskCreated.Header().Get("ETag"))
	var task v1TaskJSON
	decodeJSON(t, taskCreated, &task)
	cleanupTaskRow(t, db, task.ID)

	taskUpdated := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/tasks/"+strconv.FormatInt(task.Number, 10), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"title": "Versioned task updated"},
	)
	require.Equal(t, http.StatusOK, taskUpdated.Code, taskUpdated.Body.String())
	require.Equal(t, `"2"`, taskUpdated.Header().Get("ETag"))
	decodeJSON(t, taskUpdated, &task)
	require.Equal(t, int64(2), task.Version)

	staleTask := doWithHeaders(
		t, handler, http.MethodPatch, "/api/v1/tasks/"+strconv.FormatInt(task.Number, 10), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"title": "stale"},
	)
	assertVersionConflict(t, staleTask, 2)

	missingPrecondition := do(
		t, handler, http.MethodPatch, "/api/v1/tasks/"+strconv.FormatInt(task.Number, 10),
		userA, map[string]any{"title": "missing"},
	)
	require.Equal(t, http.StatusPreconditionRequired, missingPrecondition.Code)
	var missingProblem baseapi.Problem
	decodeJSON(t, missingPrecondition, &missingProblem)
	require.Equal(t, "PRECONDITION_REQUIRED", missingProblem.Code)

	commentCreated := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/comments", userA,
		http.Header{"If-Match": {`"2"`}},
		map[string]any{"body": "First comment"},
	)
	require.Equal(t, http.StatusCreated, commentCreated.Code, commentCreated.Body.String())
	require.Equal(t, `"1"`, commentCreated.Header().Get("ETag"))
	var comment v1CommentJSON
	decodeJSON(t, commentCreated, &comment)

	taskAfterComment := do(
		t, handler, http.MethodGet, "/api/v1/tasks/"+strconv.FormatInt(task.Number, 10),
		userA, nil,
	)
	require.Equal(t, http.StatusOK, taskAfterComment.Code, taskAfterComment.Body.String())
	require.Equal(t, `"3"`, taskAfterComment.Header().Get("ETag"))

	commentUpdated := doWithHeaders(
		t, handler, http.MethodPatch,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/comments/"+comment.ID.String(), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"body": "Edited comment"},
	)
	require.Equal(t, http.StatusOK, commentUpdated.Code, commentUpdated.Body.String())
	require.Equal(t, `"2"`, commentUpdated.Header().Get("ETag"))

	for _, path := range []string{
		"/api/v1/tasks?limit=1&sort=updated_at&order=desc",
		"/api/v1/tasks/" + strconv.FormatInt(task.Number, 10) + "/comments",
		"/api/v1/tasks/" + strconv.FormatInt(task.Number, 10) + "/activity",
		"/api/v1/labels",
	} {
		listed := do(t, handler, http.MethodGet, path, userA, nil)
		require.Equal(t, http.StatusOK, listed.Code, "%s: %s", path, listed.Body.String())
	}

	staleComment := doWithHeaders(
		t, handler, http.MethodPatch,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/comments/"+comment.ID.String(), userA,
		http.Header{"If-Match": {`"1"`}},
		map[string]any{"body": "stale"},
	)
	assertVersionConflict(t, staleComment, 2)

	archived := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/archive", userA,
		http.Header{"If-Match": {`"3"`}}, nil,
	)
	require.Equal(t, http.StatusOK, archived.Code, archived.Body.String())
	require.Equal(t, `"4"`, archived.Header().Get("ETag"))

	restored := doWithHeaders(
		t, handler, http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/restore", userA,
		http.Header{"If-Match": {`"4"`}}, nil,
	)
	require.Equal(t, http.StatusOK, restored.Code, restored.Body.String())
	require.Equal(t, `"5"`, restored.Header().Get("ETag"))

	commentDeleted := doWithHeaders(
		t, handler, http.MethodDelete,
		"/api/v1/tasks/"+strconv.FormatInt(task.Number, 10)+"/comments/"+comment.ID.String(), userA,
		http.Header{"If-Match": {`"2"`}}, nil,
	)
	require.Equal(t, http.StatusNoContent, commentDeleted.Code, commentDeleted.Body.String())

	labelDeleted := doWithHeaders(
		t, handler, http.MethodDelete, "/api/v1/labels/"+label.ID.String(), userA,
		http.Header{"If-Match": {`"2"`}}, nil,
	)
	require.Equal(t, http.StatusNoContent, labelDeleted.Code, labelDeleted.Body.String())

	taskAfterLabelDelete := do(
		t, handler, http.MethodGet, "/api/v1/tasks/"+strconv.FormatInt(task.Number, 10),
		userA, nil,
	)
	require.Equal(t, http.StatusOK, taskAfterLabelDelete.Code, taskAfterLabelDelete.Body.String())
	require.Equal(t, `"6"`, taskAfterLabelDelete.Header().Get("ETag"))
	decodeJSON(t, taskAfterLabelDelete, &task)
	require.Empty(t, task.Labels)
}

func assertVersionConflict(t *testing.T, response interface {
	Result() *http.Response
}, currentVersion int64) {
	t.Helper()
	result := response.Result()
	defer result.Body.Close()
	require.Equal(t, http.StatusPreconditionFailed, result.StatusCode)
	var problem baseapi.Problem
	require.NoError(t, json.NewDecoder(result.Body).Decode(&problem))
	require.Equal(t, "VERSION_CONFLICT", problem.Code)
	require.NotNil(t, problem.CurrentVersion)
	require.Equal(t, currentVersion, *problem.CurrentVersion)
}
