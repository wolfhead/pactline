package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type responseRepositoryStub struct {
	run   pactagent.Run
	calls map[string]pactagent.ToolCall
}

func (s *responseRepositoryStub) GetRun(
	context.Context,
	uuid.UUID,
) (pactagent.Run, error) {
	return s.run, nil
}

func (s *responseRepositoryStub) GetCompletedToolCall(
	_ context.Context,
	_ uuid.UUID,
	toolCallID string,
) (pactagent.ToolCall, error) {
	call, ok := s.calls[toolCallID]
	if !ok {
		return pactagent.ToolCall{}, pactagent.ErrToolEvidenceNotFound
	}
	return call, nil
}

func (*responseRepositoryStub) AddContextMessages(
	context.Context,
	uuid.UUID,
	string,
	int,
	time.Time,
) (int, error) {
	panic("unexpected AddContextMessages call")
}

func (*responseRepositoryStub) AttachTask(
	context.Context,
	uuid.UUID,
	string,
	uuid.UUID,
	int64,
	time.Time,
) (uuid.UUID, int64, bool, error) {
	panic("unexpected AttachTask call")
}

func TestRespondRequiresCompatibleEvidenceAndStoresStructuredSelection(t *testing.T) {
	runID := uuid.New()
	result, err := json.Marshal(TaskDetail{
		TaskSummary: TaskSummary{Number: 42, Title: "Inspect status", Status: "in_progress"},
	})
	require.NoError(t, err)
	repository := &responseRepositoryStub{
		run: pactagent.Run{ID: runID},
		calls: map[string]pactagent.ToolCall{
			"call-task": {
				RunID: runID, ToolCallID: "call-task", ToolName: ToolGetTask,
				State: pactagent.ToolCallCompleted, Result: result,
			},
		},
	}
	respond := &RespondTool{
		config: Config{Run: repository.run, Repository: repository},
		state:  &responseState{},
	}

	output, err := respond.InvokableRun(
		context.Background(),
		`{"response_type":"task_detail","source_tool_call_ids":["call-task"],"summary":"Work is active."}`,
	)

	require.NoError(t, err)
	require.JSONEq(t, `{
		"accepted":true,
		"response_type":"task_detail",
		"source_tool_call_ids":["call-task"]
	}`, output)
	selection, ok := respond.LastResponse()
	require.True(t, ok)
	require.Equal(t, ResponseTaskDetail, selection.Type)
	require.Equal(t, int64(42), selection.TaskDetail.Number)
	require.Equal(t, "Work is active.", selection.Summary)
}

func TestRespondRejectsMismatchedEvidenceAndSecondTerminalResponse(t *testing.T) {
	runID := uuid.New()
	result, err := json.Marshal(ProjectOverview{ProjectNumber: 7, ProjectName: "Launch"})
	require.NoError(t, err)
	repository := &responseRepositoryStub{
		run: pactagent.Run{ID: runID},
		calls: map[string]pactagent.ToolCall{
			"call-project": {
				RunID: runID, ToolCallID: "call-project",
				ToolName: ToolGetProjectOverview, State: pactagent.ToolCallCompleted,
				Result: result,
			},
		},
	}
	respond := &RespondTool{
		config: Config{Run: repository.run, Repository: repository},
		state:  &responseState{},
	}

	_, err = respond.InvokableRun(
		context.Background(),
		`{"response_type":"task_detail","source_tool_call_ids":["call-project"]}`,
	)
	require.ErrorIs(t, err, pactagent.ErrToolCallProtocol)

	_, err = respond.InvokableRun(
		context.Background(),
		`{"response_type":"project_status","source_tool_call_ids":["call-project"]}`,
	)
	require.NoError(t, err)
	_, err = respond.InvokableRun(
		context.Background(),
		`{"response_type":"general_response","message":"another response"}`,
	)
	require.ErrorIs(t, err, pactagent.ErrToolCallProtocol)
}

func TestGeneralResponseIsUnrestrictedButCannotReplaceCreatedTaskReceipt(t *testing.T) {
	runID := uuid.New()
	repository := &responseRepositoryStub{
		run:   pactagent.Run{ID: runID},
		calls: map[string]pactagent.ToolCall{},
	}
	respond := &RespondTool{
		config: Config{Run: repository.run, Repository: repository},
		state:  &responseState{},
	}

	_, err := respond.InvokableRun(
		context.Background(),
		`{"response_type":"general_response","message":"Any free response."}invalid`,
	)
	require.Error(t, err)

	_, err = respond.InvokableRun(
		context.Background(),
		`{"response_type":"general_response","message":"Any free response, including Task and Project facts."}`,
	)
	require.NoError(t, err)

	taskID := uuid.New()
	taskNumber := int64(8)
	repository.run.CreatedTaskID = &taskID
	repository.run.CreatedTaskNumber = &taskNumber
	respond = &RespondTool{
		config: Config{Run: repository.run, Repository: repository},
		state:  &responseState{},
	}
	_, err = respond.InvokableRun(
		context.Background(),
		`{"response_type":"general_response","message":"The Task was created."}`,
	)
	require.ErrorIs(t, err, pactagent.ErrToolCallProtocol)
}

func TestNewSetExposesOnlyAcceptedBusinessAndResponseTools(t *testing.T) {
	run := pactagent.Run{ID: uuid.New()}
	set, err := NewSet(Config{
		Run: run, WorkerID: "worker-1",
		Client: &readClientStub{},
		Repository: &responseRepositoryStub{
			run: run, calls: map[string]pactagent.ToolCall{},
		},
	})
	require.NoError(t, err)

	var names []string
	for _, configured := range set.Tools {
		info, infoErr := configured.Info(context.Background())
		require.NoError(t, infoErr)
		names = append(names, info.Name)
	}
	require.ElementsMatch(t, []string{
		ToolSearchProjects,
		ToolSearchUsers,
		ToolSearchTasks,
		ToolGetTask,
		ToolGetProjectOverview,
		ToolGetMilestoneOverview,
		ToolCreateTask,
		ToolRespond,
	}, names)
	require.NotContains(t, names, ToolAskUser)
}
