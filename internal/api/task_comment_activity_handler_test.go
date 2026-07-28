package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCommentCreateListEditDeleteHTTP(t *testing.T) {
	h, db := newTaskTestServer(t)
	task := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "commented task " + uuid.NewString()})

	rec := do(t, h, http.MethodPost, fmt.Sprintf("/api/tasks/%d/comments", task.Number), userC, map[string]any{"body": "first remark"})
	require.Equal(t, http.StatusCreated, rec.Code)
	var c commentResponse
	decodeJSON(t, rec, &c)
	require.Equal(t, uuid.MustParse(userC), c.AuthorID)

	rec = do(t, h, http.MethodGet, fmt.Sprintf("/api/tasks/%d/comments", task.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []commentResponse
	decodeJSON(t, rec, &list)
	require.Len(t, list, 1)

	// Only the author may edit.
	editPath := fmt.Sprintf("/api/tasks/%d/comments/%s", task.Number, c.ID)
	rec = do(t, h, http.MethodPatch, editPath, userD, map[string]any{"body": "hijacked"})
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = do(t, h, http.MethodPatch, editPath, userC, map[string]any{"body": "edited remark"})
	require.Equal(t, http.StatusOK, rec.Code)
	var edited commentResponse
	decodeJSON(t, rec, &edited)
	require.Equal(t, "edited remark", edited.Body)

	// Only the author may delete.
	rec = do(t, h, http.MethodDelete, editPath, userD, nil)
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = do(t, h, http.MethodDelete, editPath, userC, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = do(t, h, http.MethodGet, fmt.Sprintf("/api/tasks/%d/comments", task.Number), userA, nil)
	decodeJSON(t, rec, &list)
	require.Empty(t, list)
}

// TestActivityListRecordsCreationAndStatusChange pins that the activity
// endpoint reflects real mutations end-to-end through HTTP: creation, then
// a PATCH that changes status, must both show up, in order.
func TestActivityListRecordsCreationAndStatusChange(t *testing.T) {
	h, db := newTaskTestServer(t)
	task := mustCreateTaskHTTP(t, h, db, userA, map[string]any{"title": "activity task " + uuid.NewString()})

	rec := do(t, h, http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.Number), userB, map[string]any{"status": "in_progress"})
	require.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, h, http.MethodGet, fmt.Sprintf("/api/tasks/%d/activity", task.Number), userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var entries []activityResponse
	decodeJSON(t, rec, &entries)
	require.Len(t, entries, 2)
	require.Equal(t, "created", entries[0].Field)
	require.Equal(t, "status", entries[1].Field)
	require.Equal(t, uuid.MustParse(userB), entries[1].ActorID)
	require.Equal(t, "todo", *entries[1].OldValue)
	require.Equal(t, "in_progress", *entries[1].NewValue)
}

func TestActivityListMissingTaskReturns404(t *testing.T) {
	h, _ := newTaskTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/tasks/999999998/activity", userA, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
