package store_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/blob"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMessageConsumptionIsIdempotentByConsumerAndEvent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	outbox := store.NewOutboxStore(db)
	eventID := uuid.New()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM message_consumptions WHERE event_id=$1`, eventID)
		require.NoError(t, err)
	})
	first, err := outbox.ConsumeOnce(ctx, "test-consumer", eventID, json.RawMessage(`{"event":"test"}`))
	require.NoError(t, err)
	require.True(t, first)
	first, err = outbox.ConsumeOnce(ctx, "test-consumer", eventID, json.RawMessage(`{"event":"test"}`))
	require.NoError(t, err)
	require.False(t, first)
}

func TestThreadedCommentsValidateMentionsAndCreateDeduplicatedOutboxEvents(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tasks := store.NewTaskStore(db)
	comments := store.NewCommentStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{Title: "Threaded discussion", CreatorID: userA}, nil)
	cleanupTask(t, db, task.Task.ID)

	_, err := db.Pool.Exec(ctx, `UPDATE users SET active=true WHERE id=ANY($1)`, []uuid.UUID{userC, userD})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `UPDATE users SET active=false WHERE id=ANY($1)`, []uuid.UUID{userC, userD})
		require.NoError(t, cleanupErr)
	})
	_, err = db.Pool.Exec(ctx, `INSERT INTO project_memberships (id, project_id, user_id, role)
		VALUES ($1,$2,$3,'member'),($4,$2,$5,'member') ON CONFLICT DO NOTHING`,
		uuid.New(), task.Task.ProjectID, userC, uuid.New(), userD)
	require.NoError(t, err)

	root, err := comments.CreateVersionedThreadedWithOperation(
		ctx, task.Task.ID, task.Task.Version, userA, "Please review this plan", nil,
		[]uuid.UUID{userC}, domain.SessionOperation(userA, "comment-thread-test"),
	)
	require.NoError(t, err)
	require.Equal(t, root.Comment.ID, root.Comment.ThreadRootID)
	require.Equal(t, []uuid.UUID{userC}, root.Comment.MentionedUserIDs)

	reply, err := comments.CreateVersionedThreadedWithOperation(
		ctx, task.Task.ID, root.TaskVersion, userD, "I will take it", &root.Comment.ID,
		[]uuid.UUID{userA}, domain.SessionOperation(userD, "comment-thread-test"),
	)
	require.NoError(t, err)
	require.Equal(t, root.Comment.ID, reply.Comment.ThreadRootID)

	var mentioned, replied int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE event_type='comment.mentioned'),
		count(*) FILTER (WHERE event_type='comment.replied')
		FROM outbox_events WHERE aggregate_id=ANY($1)`, []uuid.UUID{root.Comment.ID, reply.Comment.ID}).Scan(&mentioned, &replied))
	require.Equal(t, 2, mentioned, "an explicit mention wins over the implicit reply event")
	require.Zero(t, replied)

	_, err = comments.UpdateVersionedMentionedWithOperation(
		ctx, task.Task.ID, root.Comment.ID, root.Comment.Version, "Please review the updated plan",
		[]uuid.UUID{userC, userD}, domain.SessionOperation(userA, "comment-thread-test"),
	)
	require.NoError(t, err)
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events
		WHERE aggregate_id=$1 AND event_type='comment.mentioned'`, root.Comment.ID).Scan(&mentioned))
	require.Equal(t, 2, mentioned, "editing emits only a newly added mention")

	require.NoError(t, comments.DeleteVersionedWithOperation(
		ctx, task.Task.ID, root.Comment.ID, root.Comment.Version+1,
		domain.SessionOperation(userA, "comment-thread-test"),
	))
	listed, err := comments.List(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.NotNil(t, listed[0].DeletedAt)
	require.Empty(t, listed[0].Body)
	require.Equal(t, root.Comment.ID, listed[1].ThreadRootID)

	_, err = comments.CreateVersionedThreadedWithOperation(
		ctx, task.Task.ID, reply.TaskVersion, userA, "invalid mention", nil,
		[]uuid.UUID{uuid.New()}, domain.SessionOperation(userA, "comment-thread-test"),
	)
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id=ANY($1)`, []uuid.UUID{root.Comment.ID, reply.Comment.ID})
		require.NoError(t, cleanupErr)
	})
}

func TestLocalAttachmentUploadCompletesAgainstTaskVersion(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tasks := store.NewTaskStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{Title: "Attachment flow", CreatorID: userA}, nil)
	cleanupTask(t, db, task.Task.ID)
	objects, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	service := application.AttachmentService{
		Attachments: store.NewAttachmentStore(db), Objects: objects,
		Now: func() time.Time { return time.Now().UTC() },
	}

	upload, err := service.StartUpload(ctx, task.Task.ID, userA, "prototype.html", "text/html", 13)
	require.NoError(t, err)
	require.False(t, upload.Instruction.Direct)
	require.NoError(t, service.UploadLocal(
		ctx, upload.Session, userA, strings.NewReader("<h1>Test</h1>"), 13,
	))
	completed, err := service.CompleteUpload(
		ctx, upload.Session.ID, userA, task.Task.Version,
		domain.SessionOperation(userA, "attachment-test"),
	)
	require.NoError(t, err)
	require.Equal(t, domain.AttachmentPreviewHTML, domain.AttachmentPreview(
		completed.Attachment.Filename, completed.Attachment.MediaType,
	))

	body, err := service.Open(ctx, completed.Attachment)
	require.NoError(t, err)
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, "<h1>Test</h1>", string(content))

	_, err = service.CompleteUpload(
		ctx, upload.Session.ID, userA, task.Task.Version,
		domain.SessionOperation(userA, "attachment-test"),
	)
	require.ErrorIs(t, err, domain.ErrNotFound, "an upload session completes at most once")
	deleted, err := service.Attachments.SoftDelete(
		ctx, task.Task.ID, completed.Attachment.ID, completed.Attachment.Version,
	)
	require.NoError(t, err)
	require.NotNil(t, deleted.DeletedAt)
	require.NoError(t, (application.AttachmentCleanup{
		Attachments: service.Attachments, Objects: objects,
	}).RunOnce(ctx))
	_, _, err = objects.Open(ctx, completed.Attachment.ObjectKey)
	require.Error(t, err)
	var cleanedAt *time.Time
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT cleaned_at FROM task_attachments WHERE id=$1`, completed.Attachment.ID,
	).Scan(&cleanedAt))
	require.NotNil(t, cleanedAt)
}
