package api_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	agentopenapi "github.com/wolfhead/pactline/internal/agent/openapi"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFirstPartyAgentCreatesTaskThroughDelegatedOpenAPIWithAudit(t *testing.T) {
	handler, db := newTaskTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider:            "lark",
		TenantID:            "tenant-first-party-agent",
		ConversationID:      "chat-first-party-agent",
		TriggerMessageID:    "message-" + uuid.NewString(),
		ProviderEventID:     "event-" + uuid.NewString(),
		TriggerOccurredAt:   now,
		InitiatingUserID:    uuid.MustParse(userA),
		InitiatingSubjectID: "ou_first_party_agent",
		CommandKind:         pactagent.CommandDirect,
		Model:               "deepseek-v4-pro",
		PromptVersion:       "first-party-task-v1",
	}, now)
	require.NoError(t, err)
	runs := store.NewAgentStore(db)
	_, _, err = runs.CreateRun(ctx, run)
	require.NoError(t, err)
	_, claimed, err := runs.ClaimRun(ctx, "agent-test-worker", now, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	var taskID uuid.UUID
	t.Cleanup(func() {
		ctx := context.Background()
		for _, statement := range []string{
			`DELETE FROM api_request_audit_events WHERE agent_run_id=$1`,
			`DELETE FROM business_audit_events WHERE agent_run_id=$1`,
			`DELETE FROM idempotency_records WHERE agent_run_id=$1`,
		} {
			_, cleanupErr := db.Pool.Exec(ctx, statement, run.ID)
			require.NoError(t, cleanupErr)
		}
		_, cleanupErr := db.Pool.Exec(ctx, `DELETE FROM tasks WHERE id=$1`, taskID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(ctx, `DELETE FROM agent_runs WHERE id=$1`, run.ID)
		require.NoError(t, cleanupErr)
	})

	factory, err := agentopenapi.NewFactory(newTestDelegateService(t, db), handler)
	require.NoError(t, err)
	client, err := factory.New(run.ID, run.InitiatingUserID)
	require.NoError(t, err)
	request := &generated.TaskCreate{
		Title:          "Create a delegated Agent Task",
		Context:        "The first-party Agent must use the public API boundary.",
		ExpectedResult: "The Task and its provenance are durably audited.",
		ProjectNumber:  activeProjectNumber(t, db),
	}
	key := generated.NewOptString(pactagent.CreateTaskIdempotencyKey(run.ID))
	result, err := client.CreateTask(ctx, request, generated.CreateTaskParams{
		IdempotencyKey: key,
	})
	require.NoError(t, err)
	created, ok := result.(*generated.TaskCreatedHeaders)
	require.True(t, ok, "unexpected response %T", result)
	taskID = created.Response.ID

	replayResult, err := client.CreateTask(ctx, request, generated.CreateTaskParams{
		IdempotencyKey: key,
	})
	require.NoError(t, err)
	replay, ok := replayResult.(*generated.TaskCreatedHeaders)
	require.True(t, ok)
	require.Equal(t, taskID, replay.Response.ID)
	require.True(t, replay.IdempotencyReplayed.Or(false))

	attachmentBody := []byte("<h1>Agent evidence</h1>")
	uploadResult, err := client.CreateTaskAttachmentUpload(
		ctx,
		&generated.TaskAttachmentUploadWrite{
			Filename: "evidence.html", MediaType: "text/html",
			SizeBytes: int64(len(attachmentBody)),
		},
		generated.CreateTaskAttachmentUploadParams{
			Number:         created.Response.Number,
			IdempotencyKey: generated.NewOptString("agent-test-attachment-start"),
		},
	)
	require.NoError(t, err)
	upload, ok := uploadResult.(*generated.TaskAttachmentUploadCreatedHeaders)
	require.True(t, ok, "unexpected response %T", uploadResult)
	uploaded, err := client.UploadTaskAttachmentContent(
		ctx,
		generated.UploadTaskAttachmentContentReq{Data: bytes.NewReader(attachmentBody)},
		generated.UploadTaskAttachmentContentParams{
			Number: created.Response.Number, ID: upload.Response.ID,
			ContentLength: int64(len(attachmentBody)),
		},
	)
	require.NoError(t, err)
	if problem, failed := uploaded.(*generated.ProblemStatusCodeWithHeaders); failed {
		t.Fatalf("attachment upload failed: status=%d code=%s detail=%s",
			problem.StatusCode, problem.Response.Code, problem.Response.Detail)
	}
	_, ok = uploaded.(*generated.NoContent)
	require.True(t, ok, "unexpected response %T", uploaded)
	completedResult, err := client.CompleteTaskAttachmentUpload(
		ctx,
		generated.CompleteTaskAttachmentUploadParams{
			Number: created.Response.Number, ID: upload.Response.ID,
			IfMatch:        `"1"`,
			IdempotencyKey: generated.NewOptString("agent-test-attachment-complete"),
		},
	)
	require.NoError(t, err)
	completed, ok := completedResult.(*generated.TaskAttachmentCreatedHeaders)
	require.True(t, ok, "unexpected response %T", completedResult)
	require.Equal(t, "evidence.html", completed.Response.Filename)

	queries := []struct {
		sql      string
		withTask bool
	}{
		{sql: `SELECT count(*) FROM api_request_audit_events
		 WHERE agent_run_id=$1 AND auth_method='agent_delegate'
		   AND route_pattern='/api/v1/tasks' AND status_code=201`},
		{sql: `SELECT count(*) FROM business_audit_events
		 WHERE agent_run_id=$1 AND auth_method='agent_delegate'
		   AND entity_id=$2 AND action='created'`, withTask: true},
		{sql: `SELECT count(*) FROM task_activity
		 WHERE task_id=$2 AND agent_run_id=$1 AND auth_method='agent_delegate'`, withTask: true},
		{sql: `SELECT count(*) FROM idempotency_records
		 WHERE agent_run_id=$1 AND credential_kind='agent_delegate'
		   AND credential_id=$1`},
	}
	for _, query := range queries {
		var count int
		args := []any{run.ID}
		if query.withTask {
			args = append(args, taskID)
		}
		require.NoError(t, db.Pool.QueryRow(ctx, query.sql, args...).Scan(&count))
		require.Greater(t, count, 0, query.sql)
	}
}

func TestFirstPartyAgentUpdatesOnlyItsCurrentConversationThroughOpenAPI(t *testing.T) {
	handler, db := newTaskTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	conversationStore := store.NewAgentConversationStore(db)
	conversation, err := conversationStore.Observe(
		ctx,
		"lark",
		"tenant-current-conversation",
		"chat-current-conversation",
		"Current conversation",
		uuid.MustParse(userA),
		now,
	)
	require.NoError(t, err)
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider:               "lark",
		TenantID:               "tenant-current-conversation",
		ConversationID:         "chat-current-conversation",
		TriggerMessageID:       "message-" + uuid.NewString(),
		ProviderEventID:        "event-" + uuid.NewString(),
		TriggerOccurredAt:      now,
		InitiatingUserID:       uuid.MustParse(userA),
		InitiatingSubjectID:    "ou_current_conversation",
		CommandKind:            pactagent.CommandDirect,
		Model:                  "deepseek-v4-pro",
		PromptVersion:          "first-party-work-v12",
		ConversationRevisionID: &conversation.Revision.ID,
	}, now)
	require.NoError(t, err)
	runs := store.NewAgentStore(db)
	_, _, err = runs.CreateRun(ctx, run)
	require.NoError(t, err)
	_, claimed, err := runs.ClaimRun(ctx, "agent-configuration-worker", now, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	t.Cleanup(func() {
		cleanupContext := context.Background()
		for _, statement := range []string{
			`DELETE FROM api_request_audit_events WHERE agent_run_id=$1`,
			`DELETE FROM idempotency_records WHERE agent_run_id=$1`,
			`DELETE FROM agent_runs WHERE id=$1`,
		} {
			_, cleanupErr := db.Pool.Exec(cleanupContext, statement, run.ID)
			require.NoError(t, cleanupErr)
		}
		_, cleanupErr := db.Pool.Exec(cleanupContext,
			`DELETE FROM agent_conversation_revisions WHERE conversation_id=$1`,
			conversation.Conversation.ID,
		)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupContext,
			`DELETE FROM agent_conversations WHERE id=$1`,
			conversation.Conversation.ID,
		)
		require.NoError(t, cleanupErr)
	})

	factory, err := agentopenapi.NewFactory(newTestDelegateService(t, db), handler)
	require.NoError(t, err)
	client, err := factory.New(run.ID, run.InitiatingUserID)
	require.NoError(t, err)
	currentResult, err := client.GetCurrentAgentConversationConfiguration(ctx)
	require.NoError(t, err)
	current, ok := currentResult.(*generated.AgentConversationHeaders)
	require.True(t, ok, "unexpected response %T", currentResult)
	require.Equal(t, conversation.Conversation.ID, current.Response.ID)
	require.True(t, current.Response.CanManage)

	projectNumber := activeProjectNumber(t, db)
	updateResult, err := client.UpdateCurrentAgentConversationConfiguration(
		ctx,
		&generated.AgentConversationPatch{
			DefaultProjectNumber: generated.NewOptInt64(projectNumber),
		},
		generated.UpdateCurrentAgentConversationConfigurationParams{
			IfMatch: current.Etag.Value,
			IdempotencyKey: generated.NewOptString(
				pactagent.ConversationConfigurationIdempotencyKey(run.ID),
			),
		},
	)
	require.NoError(t, err)
	updated, ok := updateResult.(*generated.AgentConversationHeaders)
	require.True(t, ok, "unexpected response %T", updateResult)
	project, ok := updated.Response.DefaultProject.Get()
	require.True(t, ok)
	require.Equal(t, projectNumber, project.Number)
	require.True(t, updated.Response.BindingActive)
	require.Equal(t, current.Response.Version+1, updated.Response.Version)
}
