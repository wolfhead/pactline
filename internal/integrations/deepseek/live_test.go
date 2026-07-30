package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

const liveTestTimeout = 3 * time.Minute

func init() {
	schema.RegisterName[askForTitleState]("pactline_deepseek_live_ask_for_title_state")
}

func TestLiveDeepSeekSequentialToolCalls(t *testing.T) {
	apiKey := liveAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	model, err := NewChatModel(ctx, liveConfig(apiKey))
	require.NoError(t, err)

	var (
		mu        sync.Mutex
		callOrder []string
	)
	resolveProject, err := toolutils.InferTool(
		"resolve_project",
		"Resolve the only allowed project before creating a task.",
		func(_ context.Context, input resolveProjectInput) (resolveProjectOutput, error) {
			mu.Lock()
			callOrder = append(callOrder, "resolve_project")
			mu.Unlock()
			if input.Name != "Pactline" {
				return resolveProjectOutput{}, fmt.Errorf("project %q does not exist", input.Name)
			}
			return resolveProjectOutput{ProjectNumber: 42}, nil
		},
	)
	require.NoError(t, err)

	dryRunCreateTask, err := toolutils.InferTool(
		"dry_run_create_task",
		"Validate one task creation after the project has been resolved.",
		func(_ context.Context, input dryRunCreateTaskInput) (dryRunCreateTaskOutput, error) {
			mu.Lock()
			callOrder = append(callOrder, "dry_run_create_task")
			mu.Unlock()
			if input.ProjectNumber != 42 {
				return dryRunCreateTaskOutput{}, fmt.Errorf("unexpected project number %d", input.ProjectNumber)
			}
			if strings.TrimSpace(input.Title) == "" {
				return dryRunCreateTaskOutput{}, fmt.Errorf("title is required")
			}
			return dryRunCreateTaskOutput{
				TaskNumber: 101,
				Title:      input.Title,
			}, nil
		},
	)
	require.NoError(t, err)

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "deepseek-compatibility",
		Instruction: `You are a deterministic compatibility test.
You must call resolve_project exactly once with {"name":"Pactline"}.
Only after its result, call dry_run_create_task exactly once with project_number 42 and title "DeepSeek compatibility gate".
Do not call both tools in the same model response.
After both tools succeed, answer exactly LIVE_TOOL_GATE_OK.`,
		Model: model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{resolveProject, dryRunCreateTask},
			},
		},
		MaxIterations: 6,
	})
	require.NoError(t, err)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	events := drainAgentEvents(t, runner.Query(ctx, "Run the compatibility test now."))

	mu.Lock()
	gotCallOrder := append([]string(nil), callOrder...)
	mu.Unlock()
	require.Equal(t, []string{"resolve_project", "dry_run_create_task"}, gotCallOrder)
	require.Contains(t, finalAssistantContent(t, events), "LIVE_TOOL_GATE_OK")

}

func TestLiveDeepSeekCheckpointResumeAfterRunnerReconstruction(t *testing.T) {
	apiKey := liveAPIKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	store := newMemoryCheckpointStore()
	checkpointID := "deepseek-live-restart-gate"
	var resumed bool

	makeRunner := func() *adk.Runner {
		model, err := NewChatModel(ctx, liveConfig(apiKey))
		require.NoError(t, err)

		clarifyTool, toolErr := toolutils.InferTool(
			"ask_for_title",
			"Ask the initiating user for the missing task title and then consume the resumed answer.",
			func(toolCtx context.Context, input askForTitleInput) (askForTitleOutput, error) {
				wasInterrupted, hasState, state := tool.GetInterruptState[askForTitleState](toolCtx)
				if !wasInterrupted {
					return askForTitleOutput{}, tool.StatefulInterrupt(
						toolCtx,
						"Please provide the one task title.",
						askForTitleState{Question: input.Question},
					)
				}
				if !hasState || state.Question == "" {
					return askForTitleOutput{}, fmt.Errorf("clarification checkpoint state is missing")
				}
				isTarget, hasData, answer := tool.GetResumeContext[string](toolCtx)
				if !isTarget {
					return askForTitleOutput{}, tool.StatefulInterrupt(
						toolCtx,
						"Please provide the one task title.",
						state,
					)
				}
				if !hasData || strings.TrimSpace(answer) == "" {
					return askForTitleOutput{}, fmt.Errorf("clarification answer is missing")
				}
				resumed = true
				return askForTitleOutput{Title: strings.TrimSpace(answer)}, nil
			},
		)
		require.NoError(t, toolErr)

		agent, agentErr := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
			Name: "deepseek-resume-compatibility",
			Instruction: `You are a deterministic checkpoint compatibility test.
Call ask_for_title exactly once with a concise question.
When the tool returns a title after resume, answer exactly LIVE_RESUME_GATE_OK followed by one space and that title.`,
			Model: model,
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{
					Tools: []tool.BaseTool{clarifyTool},
				},
			},
			MaxIterations: 4,
		})
		require.NoError(t, agentErr)
		return adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           agent,
			CheckPointStore: store,
		})
	}

	firstRunner := makeRunner()
	firstEvents := drainAgentEvents(t, firstRunner.Query(
		ctx,
		"Start the checkpoint test.",
		adk.WithCheckPointID(checkpointID),
	))
	interruptID := rootInterruptID(t, firstEvents)

	// Reconstructing the model, agent, and runner while retaining only the
	// checkpoint store simulates a worker process restart.
	secondRunner := makeRunner()
	resumeIterator, err := secondRunner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: "Recovered task title",
		},
	})
	require.NoError(t, err)
	resumeEvents := drainAgentEvents(t, resumeIterator)

	require.True(t, resumed)
	require.Contains(t, finalAssistantContent(t, resumeEvents), "LIVE_RESUME_GATE_OK Recovered task title")
}

type resolveProjectInput struct {
	Name string `json:"name" jsonschema:"required,description=Exact project name"`
}

type resolveProjectOutput struct {
	ProjectNumber int `json:"project_number"`
}

type dryRunCreateTaskInput struct {
	ProjectNumber int    `json:"project_number" jsonschema:"required,description=Resolved project number"`
	Title         string `json:"title" jsonschema:"required,description=Task title"`
}

type dryRunCreateTaskOutput struct {
	TaskNumber int    `json:"task_number"`
	Title      string `json:"title"`
}

type askForTitleInput struct {
	Question string `json:"question" jsonschema:"required,description=Question shown to the initiating user"`
}

type askForTitleState struct {
	Question string
}

type askForTitleOutput struct {
	Title string `json:"title"`
}

type memoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryCheckpointStore() *memoryCheckpointStore {
	return &memoryCheckpointStore{data: make(map[string][]byte)}
}

func (s *memoryCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[id]
	return append([]byte(nil), value...), ok, nil
}

func (s *memoryCheckpointStore) Set(_ context.Context, id string, checkpoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = append([]byte(nil), checkpoint...)
	return nil
}

func (s *memoryCheckpointStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func liveAPIKey(t *testing.T) string {
	t.Helper()
	apiKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY is not set; skipping live compatibility gate")
	}
	return apiKey
}

func liveConfig(apiKey string) Config {
	return Config{
		APIKey:  apiKey,
		BaseURL: strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL")),
		Model:   strings.TrimSpace(os.Getenv("DEEPSEEK_MODEL")),
		Timeout: liveTestTimeout,
	}
}

func drainAgentEvents(t *testing.T, iterator *adk.AsyncIterator[*adk.AgentEvent]) []*adk.AgentEvent {
	t.Helper()
	var events []*adk.AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		require.NotNil(t, event)
		require.NoError(t, event.Err)
		events = append(events, event)
	}
	return events
}

func finalAssistantContent(t *testing.T, events []*adk.AgentEvent) string {
	t.Helper()
	var content string
	for _, event := range events {
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		require.NoError(t, err)
		if message != nil && message.Role == schema.Assistant && strings.TrimSpace(message.Content) != "" {
			content = strings.TrimSpace(message.Content)
		}
	}
	require.NotEmpty(t, content)
	return content
}

func rootInterruptID(t *testing.T, events []*adk.AgentEvent) string {
	t.Helper()
	for _, event := range events {
		if event.Action == nil || event.Action.Interrupted == nil {
			continue
		}
		for _, interruptContext := range event.Action.Interrupted.InterruptContexts {
			if interruptContext.IsRootCause {
				require.NotEmpty(t, interruptContext.ID)
				return interruptContext.ID
			}
		}
	}
	require.FailNow(t, "root interrupt was not emitted")
	return ""
}

func TestMemoryCheckpointStoreCopiesValues(t *testing.T) {
	store := newMemoryCheckpointStore()
	source := []byte("checkpoint")
	require.NoError(t, store.Set(context.Background(), "run-1", source))
	source[0] = 'X'

	first, ok, err := store.Get(context.Background(), "run-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("checkpoint"), first)

	first[0] = 'Y'
	second, ok, err := store.Get(context.Background(), "run-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("checkpoint"), second)

	encoded, err := json.Marshal(askForTitleState{Question: "question"})
	require.NoError(t, err)
	require.JSONEq(t, `{"Question":"question"}`, string(encoded))
}
