package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSourceArtifactIDsDeduplicatesAndBoundsSelection(t *testing.T) {
	values, err := normalizeSourceArtifactIDs([]string{" image ", "file", "image"})
	require.NoError(t, err)
	require.Equal(t, []string{"image", "file"}, values)

	_, err = normalizeSourceArtifactIDs([]string{"1", "2", "3", "4", "5", "6"})
	require.ErrorIs(t, err, ErrToolInput)

	values, err = normalizeSourceArtifactIDs([]string{"same", "same", "same", "same", "same", "same"})
	require.NoError(t, err)
	require.Equal(t, []string{"same"}, values)

	_, err = normalizeSourceArtifactIDs([]string{""})
	require.ErrorIs(t, err, ErrToolInput)
}

func TestAttachSourceArtifactsCopiesSelectedEvidenceAndReportsPartialFailure(t *testing.T) {
	cleaned := false
	resolver := &sourceArtifactResolverStub{
		files: map[string]artifact.LocalFile{
			"prototype": {
				Reference: artifact.Reference{
					ID: "prototype", Kind: artifact.KindFile,
					Name: "prototype.html", MediaType: "text/html",
				},
				Path: "testdata/prototype.html",
				Cleanup: func() error {
					cleaned = true
					return nil
				},
			},
		},
	}
	client := &attachmentClientStub{}
	run := pactagent.Run{
		ID: uuid.New(), TenantID: "tenant", ConversationID: "chat",
		TriggerOccurredAt: time.Now().UTC(),
	}
	tool := &CreateTaskTool{config: Config{
		Run: run, Client: client, Artifacts: resolver,
	}}

	attached, failures := tool.attachSourceArtifacts(
		context.Background(),
		CreatedTask{Number: 42},
		3,
		[]string{"prototype", "missing"},
	)

	require.Len(t, attached, 1)
	require.Equal(t, "prototype", attached[0].ArtifactID)
	require.Equal(t, "prototype.html", attached[0].Filename)
	require.Equal(t, []byte("<h1>Agent evidence</h1>\n"), client.uploadedBodies[0])
	require.Equal(t, `"3"`, client.ifMatches[0])
	require.NotEmpty(t, client.startIdempotencyKeys[0])
	require.NotEmpty(t, client.completeIdempotencyKeys[0])
	require.Len(t, failures, 1)
	require.Equal(t, "missing", failures[0].ArtifactID)
	require.Equal(t, "artifact_unavailable", failures[0].Reason)
	require.True(t, cleaned)
}

func TestCreateTaskPreservesSelectedArtifactInMutationReceipt(t *testing.T) {
	run := pactagent.Run{
		ID: uuid.New(), TenantID: "tenant", ConversationID: "chat",
		TriggerOccurredAt: time.Now().UTC(),
	}
	taskID := uuid.New()
	client := &attachmentClientStub{taskResponse: &generated.TaskCreatedHeaders{
		Response: generated.Task{
			ID: taskID, Number: 42, Version: 1, Title: "Build prototype",
			Project: generated.ProjectRef{Number: 7, Name: "Pactline"},
			Phase:   generated.TaskPhaseBacklog, Priority: generated.TaskPriorityNone,
		},
	}}
	repository := &createTaskRepositoryStub{run: run}
	tool := &CreateTaskTool{config: Config{
		Run: run, WorkerID: "worker", Client: client, Repository: repository,
		Now: time.Now,
		Artifacts: &sourceArtifactResolverStub{files: map[string]artifact.LocalFile{
			"prototype": {
				Reference: artifact.Reference{
					ID: "prototype", Kind: artifact.KindFile,
					Name: "prototype.html", MediaType: "text/html",
				},
				Path: "testdata/prototype.html",
			},
		}},
	}}

	created, err := tool.create(context.Background(), CreateTaskInput{
		Title: "Build prototype", Context: "The design was agreed in chat.",
		ExpectedResult: "The prototype is implemented.", ProjectNumber: 7,
		Priority: "none", SourceArtifactIDs: []string{"prototype", "prototype"},
	})

	require.NoError(t, err)
	require.Equal(t, taskID, created.ID)
	require.Len(t, created.AttachedArtifacts, 1)
	require.Equal(t, "prototype.html", created.AttachedArtifacts[0].Filename)
	require.Empty(t, created.AttachmentFailures)
	require.Equal(t, taskID, repository.attachedTaskID)
}

func TestUploadArtifactContentSupportsPrivateDirectCloudUpload(t *testing.T) {
	var received []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "signed-value", request.Header.Get("X-Signed-Header"))
		var err error
		received, err = io.ReadAll(request.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	source, err := artifact.PrepareAttachment(artifact.LocalFile{
		Reference: artifact.Reference{Name: "prototype.html", MediaType: "text/html"},
		Path:      "testdata/prototype.html",
	})
	require.NoError(t, err)
	tool := &CreateTaskTool{config: Config{DirectUploadHTTP: server.Client()}}

	err = tool.uploadArtifactContent(
		context.Background(),
		&attachmentClientStub{},
		42,
		generated.TaskAttachmentUpload{
			ID: uuid.New(), Direct: true, UploadURL: server.URL,
			Headers: generated.TaskAttachmentUploadHeaders{"X-Signed-Header": "signed-value"},
		},
		source,
	)

	require.NoError(t, err)
	require.Equal(t, []byte("<h1>Agent evidence</h1>\n"), received)
}

type sourceArtifactResolverStub struct {
	files map[string]artifact.LocalFile
}

func (s *sourceArtifactResolverStub) Resolve(
	_ context.Context,
	_ artifact.Scope,
	id string,
) (artifact.LocalFile, error) {
	file, ok := s.files[id]
	if !ok {
		return artifact.LocalFile{}, artifact.ErrNotFound
	}
	return file, nil
}

type attachmentClientStub struct {
	uploads                 []generated.TaskAttachmentUpload
	uploadedBodies          [][]byte
	ifMatches               []string
	startIdempotencyKeys    []string
	completeIdempotencyKeys []string
	taskResponse            generated.CreateTaskRes
}

func (s *attachmentClientStub) CreateTaskAttachmentUpload(
	_ context.Context,
	request *generated.TaskAttachmentUploadWrite,
	params generated.CreateTaskAttachmentUploadParams,
) (generated.CreateTaskAttachmentUploadRes, error) {
	upload := generated.TaskAttachmentUpload{
		ID: uuid.New(), Provider: generated.TaskAttachmentUploadProviderLocal,
		Filename: request.Filename, MediaType: request.MediaType, SizeBytes: request.SizeBytes,
		Direct: false, Method: generated.TaskAttachmentUploadMethodPUT,
		UploadURL: "/upload", Headers: generated.TaskAttachmentUploadHeaders{},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	s.uploads = append(s.uploads, upload)
	s.startIdempotencyKeys = append(s.startIdempotencyKeys, params.IdempotencyKey.Or(""))
	return &generated.TaskAttachmentUploadCreatedHeaders{Response: upload}, nil
}

func (s *attachmentClientStub) UploadTaskAttachmentContent(
	_ context.Context,
	request generated.UploadTaskAttachmentContentReq,
	params generated.UploadTaskAttachmentContentParams,
) (generated.UploadTaskAttachmentContentRes, error) {
	body, err := io.ReadAll(request.Data)
	if err != nil {
		return nil, err
	}
	s.uploadedBodies = append(s.uploadedBodies, body)
	if params.ContentLength != int64(len(body)) {
		return nil, errors.New("Content-Length mismatch")
	}
	return &generated.NoContent{}, nil
}

func (s *attachmentClientStub) CompleteTaskAttachmentUpload(
	_ context.Context,
	params generated.CompleteTaskAttachmentUploadParams,
) (generated.CompleteTaskAttachmentUploadRes, error) {
	s.ifMatches = append(s.ifMatches, params.IfMatch)
	s.completeIdempotencyKeys = append(s.completeIdempotencyKeys, params.IdempotencyKey.Or(""))
	for _, upload := range s.uploads {
		if upload.ID == params.ID {
			return &generated.TaskAttachmentCreatedHeaders{Response: generated.TaskAttachment{
				ID: uuid.New(), Filename: upload.Filename,
			}}, nil
		}
	}
	return nil, errors.New("upload not found")
}

func (*attachmentClientStub) ListProjects(context.Context, generated.ListProjectsParams) (generated.ListProjectsRes, error) {
	panic("unexpected ListProjects call")
}

func (*attachmentClientStub) ListUsers(context.Context, generated.ListUsersParams) (generated.ListUsersRes, error) {
	panic("unexpected ListUsers call")
}

func (*attachmentClientStub) ListTasks(context.Context, generated.ListTasksParams) (generated.ListTasksRes, error) {
	panic("unexpected ListTasks call")
}

func (*attachmentClientStub) GetTask(context.Context, generated.GetTaskParams) (generated.GetTaskRes, error) {
	panic("unexpected GetTask call")
}

func (*attachmentClientStub) GetProject(context.Context, generated.GetProjectParams) (generated.GetProjectRes, error) {
	panic("unexpected GetProject call")
}

func (s *attachmentClientStub) CreateTask(context.Context, *generated.TaskCreate, generated.CreateTaskParams) (generated.CreateTaskRes, error) {
	if s.taskResponse == nil {
		panic("unexpected CreateTask call")
	}
	return s.taskResponse, nil
}

type createTaskRepositoryStub struct {
	run            pactagent.Run
	attachedTaskID uuid.UUID
}

func (s *createTaskRepositoryStub) GetRun(context.Context, uuid.UUID) (pactagent.Run, error) {
	return s.run, nil
}

func (*createTaskRepositoryStub) GetCompletedToolCall(context.Context, uuid.UUID, string) (pactagent.ToolCall, error) {
	panic("unexpected GetCompletedToolCall call")
}

func (*createTaskRepositoryStub) AddContextMessages(context.Context, uuid.UUID, string, int, time.Time) (int, error) {
	panic("unexpected AddContextMessages call")
}

func (s *createTaskRepositoryStub) AttachTask(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	taskID uuid.UUID,
	taskNumber int64,
	_ time.Time,
) (uuid.UUID, int64, bool, error) {
	s.attachedTaskID = taskID
	return taskID, taskNumber, true, nil
}
