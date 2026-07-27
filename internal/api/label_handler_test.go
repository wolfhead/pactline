package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLabelCreateListRenameDeleteHTTP(t *testing.T) {
	h, db := newTaskTestServer(t)
	name := "http-label-" + uuid.NewString()

	rec := do(t, h, http.MethodPost, "/api/labels", userA, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	var created labelJSON
	decodeJSON(t, rec, &created)
	cleanupLabelRow(t, db, created.ID)

	rec = do(t, h, http.MethodGet, "/api/labels", userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var all []labelJSON
	decodeJSON(t, rec, &all)
	require.True(t, containsLabelName(all, name))

	rec = do(t, h, http.MethodPatch, "/api/labels/"+created.ID.String(), userA, map[string]any{"name": name + "-renamed"})
	require.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, h, http.MethodDelete, "/api/labels/"+created.ID.String(), userA, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = do(t, h, http.MethodGet, "/api/labels", userA, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeJSON(t, rec, &all)
	require.False(t, containsLabelName(all, name+"-renamed"))
}

func TestLabelCreateDuplicateReturns409(t *testing.T) {
	h, db := newTaskTestServer(t)
	name := "http-dup-label-" + uuid.NewString()

	rec := do(t, h, http.MethodPost, "/api/labels", userA, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	var created labelJSON
	decodeJSON(t, rec, &created)
	cleanupLabelRow(t, db, created.ID)

	rec = do(t, h, http.MethodPost, "/api/labels", userA, map[string]any{"name": name})
	require.Equal(t, http.StatusConflict, rec.Code)
}

func containsLabelName(labels []labelJSON, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}
