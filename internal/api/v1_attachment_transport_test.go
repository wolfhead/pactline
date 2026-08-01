package api_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestV1PrivateLocalAttachmentFlowAndSandboxedHTML(t *testing.T) {
	handler, db := newTaskTestServer(t)
	taskCreated := do(t, handler, http.MethodPost, "/api/v1/tasks", userA, map[string]any{
		"title": "Attachment transport", "context": "Exercise private streaming",
		"expected_result": "Authorized HTML remains sandboxed",
		"project_number":  activeProjectNumber(t, db),
	})
	require.Equal(t, http.StatusCreated, taskCreated.Code, taskCreated.Body.String())
	var task v1TaskJSON
	decodeJSON(t, taskCreated, &task)
	cleanupTaskRow(t, db, task.ID)
	basePath := "/api/v1/tasks/" + strconv.FormatInt(task.Number, 10) + "/attachments"

	uploadCreated := do(t, handler, http.MethodPost, basePath+"/uploads", userA, map[string]any{
		"filename": "prototype.html", "media_type": "text/html", "size_bytes": 11,
	})
	require.Equal(t, http.StatusCreated, uploadCreated.Code, uploadCreated.Body.String())
	var upload struct {
		ID        uuid.UUID `json:"id"`
		Direct    bool      `json:"direct"`
		UploadURL string    `json:"upload_url"`
	}
	decodeJSON(t, uploadCreated, &upload)
	require.False(t, upload.Direct)

	uploaded := doRaw(t, handler, http.MethodPut, upload.UploadURL, userA, "text/html", nil, []byte("<h1>Ok</h1>"))
	require.Equal(t, http.StatusNoContent, uploaded.Code, uploaded.Body.String())

	completed := doWithHeaders(t, handler, http.MethodPost,
		basePath+"/uploads/"+upload.ID.String()+"/complete", userA,
		http.Header{"If-Match": {`"1"`}}, nil,
	)
	require.Equal(t, http.StatusCreated, completed.Code, completed.Body.String())
	var attachment struct {
		ID          uuid.UUID `json:"id"`
		PreviewKind string    `json:"preview_kind"`
		Version     int64     `json:"version"`
		ContentURL  string    `json:"content_url"`
	}
	decodeJSON(t, completed, &attachment)
	require.Equal(t, "html", attachment.PreviewKind)

	content := doRaw(t, handler, http.MethodGet, attachment.ContentURL, userA, "", nil, nil)
	require.Equal(t, http.StatusOK, content.Code, content.Body.String())
	require.Equal(t, "sandbox allow-scripts", content.Header().Get("Content-Security-Policy"))
	require.Equal(t, "nosniff", content.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "<h1>Ok</h1>", content.Body.String())

	deleted := doWithHeaders(t, handler, http.MethodDelete,
		basePath+"/"+attachment.ID.String(), userA,
		http.Header{"If-Match": {`"1"`}}, nil,
	)
	require.Equal(t, http.StatusNoContent, deleted.Code, deleted.Body.String())
	var count int
	require.NoError(t, db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM task_attachments
		WHERE task_id=$1 AND deleted_at IS NULL`, task.ID).Scan(&count))
	require.Zero(t, count)
}
