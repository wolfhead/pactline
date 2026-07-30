package tools

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"sync"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type projectClientStub struct {
	projects []generated.Project
}

func (s projectClientStub) ListProjects(
	context.Context,
	generated.ListProjectsParams,
) (generated.ListProjectsRes, error) {
	return &generated.ProjectListHeaders{
		Response: generated.ProjectList{Items: s.projects},
	}, nil
}

func (projectClientStub) ListUsers(
	context.Context,
	generated.ListUsersParams,
) (generated.ListUsersRes, error) {
	panic("unexpected ListUsers call")
}

func (projectClientStub) CreateTask(
	context.Context,
	*generated.TaskCreate,
	generated.CreateTaskParams,
) (generated.CreateTaskRes, error) {
	panic("unexpected CreateTask call")
}

func (projectClientStub) ListTasks(
	context.Context,
	generated.ListTasksParams,
) (generated.ListTasksRes, error) {
	panic("unexpected ListTasks call")
}

func (projectClientStub) GetTask(
	context.Context,
	generated.GetTaskParams,
) (generated.GetTaskRes, error) {
	panic("unexpected GetTask call")
}

func (projectClientStub) GetProject(
	context.Context,
	generated.GetProjectParams,
) (generated.GetProjectRes, error) {
	panic("unexpected GetProject call")
}

func TestSearchProjectsListsTheOnlyActiveProjectWhenQueryIsEmpty(t *testing.T) {
	result, err := searchProjects(context.Background(), projectClientStub{
		projects: []generated.Project{
			{Number: 1, Name: "测试"},
		},
	}, SearchInput{})

	require.NoError(t, err)
	require.Equal(t, []ProjectCandidate{{Number: 1, Name: "测试"}}, result.Candidates)
	require.Equal(t, &ProjectCandidate{Number: 1, Name: "测试"}, result.OnlyActiveProject)
}

func TestSearchProjectsExposesOnlyActiveProjectWhenInferredQueryDoesNotMatch(t *testing.T) {
	result, err := searchProjects(context.Background(), projectClientStub{
		projects: []generated.Project{
			{Number: 1, Name: "测试"},
		},
	}, SearchInput{Query: "Lark Agent"})

	require.NoError(t, err)
	require.Empty(t, result.Candidates)
	require.Equal(t, &ProjectCandidate{Number: 1, Name: "测试"}, result.OnlyActiveProject)
}

func TestClarificationInterruptPayloadCanBeCheckpointEncoded(t *testing.T) {
	payload := struct {
		Info any
	}{
		Info: ClarificationInfo{
			Question:   "Which Project?",
			Candidates: []string{"测试"},
		},
	}

	var encoded bytes.Buffer
	require.NoError(t, gob.NewEncoder(&encoded).Encode(payload))
}

func TestRespondAskUserPersistsCheckpointThroughEinoRunnerAndLedger(t *testing.T) {
	ctx := context.Background()
	run := pactagent.Run{ID: uuid.New()}
	clarifyTool := &RespondTool{
		config: Config{
			Run: run,
			Repository: &responseRepositoryStub{
				run: run, calls: map[string]pactagent.ToolCall{},
			},
		},
		state: &responseState{},
	}
	checkpoints := newCheckpointMemoryStore()
	ledger := pactagent.ToolLedger{
		RunID:      run.ID,
		Repository: &toolCallRepositoryStub{},
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "checkpoint-regression",
		Description: "Exercises the production clarification tool.",
		Instruction: "Call ask_user.",
		Model:       clarificationModel{},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               []tool.BaseTool{clarifyTool},
				ToolCallMiddlewares: []compose.ToolMiddleware{ledger.Middleware()},
			},
		},
		MaxIterations: 2,
	})
	require.NoError(t, err)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		CheckPointStore: checkpoints,
	})

	iterator := runner.Query(ctx, "Create one Task.", adk.WithCheckPointID("checkpoint-1"))
	var interruptID string
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		require.NotNil(t, event)
		require.NoError(t, event.Err)
		if event.Action == nil || event.Action.Interrupted == nil {
			continue
		}
		for _, interruptContext := range event.Action.Interrupted.InterruptContexts {
			if interruptContext.IsRootCause {
				interruptID = interruptContext.ID
			}
		}
	}

	require.NotEmpty(t, interruptID)
	_, exists, err := checkpoints.Get(ctx, "checkpoint-1")
	require.NoError(t, err)
	require.True(t, exists)
}

type clarificationModel struct{}

func (clarificationModel) Generate(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.Message, error) {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "ask-user-call-1",
		Type: "function",
		Function: schema.FunctionCall{
			Name: ToolRespond,
			Arguments: `{
					"response_type":"ask_user_question",
					"question":"Which Project should contain this Task?",
					"candidates":["测试"]
				}`,
		},
	}}), nil
}

func (m clarificationModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m clarificationModel) WithTools(
	[]*schema.ToolInfo,
) (model.ToolCallingChatModel, error) {
	return m, nil
}

type checkpointMemoryStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newCheckpointMemoryStore() *checkpointMemoryStore {
	return &checkpointMemoryStore{data: make(map[string][]byte)}
}

func (s *checkpointMemoryStore) Get(
	_ context.Context,
	id string,
) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, exists := s.data[id]
	return append([]byte(nil), value...), exists, nil
}

func (s *checkpointMemoryStore) Set(
	_ context.Context,
	id string,
	value []byte,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = append([]byte(nil), value...)
	return nil
}

func (s *checkpointMemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

type toolCallRepositoryStub struct{}

func (*toolCallRepositoryStub) ClaimToolCall(
	context.Context,
	pactagent.ToolCall,
) (pactagent.ToolCallClaim, error) {
	return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimAcquired}, nil
}

func (*toolCallRepositoryStub) CompleteToolCall(
	context.Context,
	uuid.UUID,
	string,
	[]byte,
	time.Time,
) error {
	return errors.New("unexpected completed interrupted tool call")
}

func (*toolCallRepositoryStub) FailToolCall(
	context.Context,
	uuid.UUID,
	string,
	string,
	time.Time,
) error {
	return errors.New("unexpected failed interrupted tool call")
}
