package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/agent/ingress"
	agentruntime "github.com/wolfhead/pactline/internal/agent/runtime"
	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/google/uuid"
)

const ArtifactVersion = 1

type RunConfig struct {
	ModelName      string
	Model          einomodel.ToolCallingChatModel
	ArtifactModel  einomodel.ToolCallingChatModel
	Timezone       *time.Location
	ArtifactVision artifact.VisionAnalyzer
}

type ConversionArtifact struct {
	Version           int                   `json:"version"`
	ScenarioID        string                `json:"scenario_id"`
	RunID             uuid.UUID             `json:"run_id"`
	Model             string                `json:"model"`
	PromptVersion     string                `json:"prompt_version"`
	CommandKind       pactagent.CommandKind `json:"command_kind"`
	Outcome           string                `json:"outcome"`
	Task              *TaskProposal         `json:"task,omitempty"`
	Clarification     *Clarification        `json:"clarification,omitempty"`
	Response          *ResponseArtifact     `json:"response,omitempty"`
	ToolTrace         []ToolTrace           `json:"tool_trace"`
	PromptTokens      int                   `json:"prompt_tokens"`
	CompletionTokens  int                   `json:"completion_tokens"`
	TotalTokens       int                   `json:"total_tokens"`
	DurationMS        int64                 `json:"duration_ms"`
	GenerationError   string                `json:"generation_error,omitempty"`
	GenerationStarted time.Time             `json:"generation_started_at"`
}

type TaskProposal struct {
	Title          string     `json:"title"`
	Context        string     `json:"context"`
	ExpectedResult string     `json:"expected_result"`
	ProjectNumber  int64      `json:"project_number"`
	MilestoneID    *uuid.UUID `json:"milestone_id,omitempty"`
	AssigneeID     *uuid.UUID `json:"assignee_id,omitempty"`
	DueDate        *string    `json:"due_date,omitempty"`
	Priority       string     `json:"priority"`
}

type Clarification struct {
	Question   string   `json:"question"`
	Candidates []string `json:"candidates,omitempty"`
}

type ResponseArtifact struct {
	Type    string `json:"type"`
	Summary string `json:"summary,omitempty"`
	Message string `json:"message,omitempty"`
}

func RunScenario(
	ctx context.Context,
	scenario Scenario,
	config RunConfig,
) (ConversionArtifact, error) {
	if err := scenario.Validate(); err != nil {
		return ConversionArtifact{}, err
	}
	if config.Model == nil || strings.TrimSpace(config.ModelName) == "" {
		return ConversionArtifact{}, errors.New("configure conversation evaluation: model is required")
	}
	if config.Timezone == nil {
		config.Timezone = time.UTC
	}
	if config.ArtifactModel == nil {
		config.ArtifactModel = config.Model
	}
	startedAt := time.Now()
	commandKind := ingress.ClassifyCommand(scenario.Trigger.Text)
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider: "lark", TenantID: "evaluation-tenant",
		ConversationID:       "evaluation:" + scenario.ID,
		TriggerMessageID:     scenario.Trigger.MessageID,
		ProviderEventID:      "evaluation-event:" + scenario.ID,
		ThreadRootMessageID:  scenario.Trigger.ThreadRootMessageID,
		ReplyParentMessageID: scenario.Trigger.ReplyToMessageID,
		TriggerOccurredAt:    scenario.Trigger.At,
		InitiatingUserID:     stableUUID(scenario.ID + ":initiator"),
		InitiatingSubjectID:  stableUUID(scenario.ID + ":sender:" + scenario.Trigger.Sender).String(),
		CommandKind:          commandKind,
		Model:                config.ModelName, PromptVersion: agentruntime.PromptVersion,
	}, scenario.Trigger.At)
	if err != nil {
		return ConversionArtifact{}, err
	}
	run.Status = pactagent.RunRunning
	run.AttemptCount = 1
	run.LeaseOwner = "evaluation"
	leaseExpiry := scenario.Trigger.At.Add(10 * time.Minute)
	run.LeaseExpiresAt = &leaseExpiry

	sandbox, err := NewSandbox(scenario, run)
	if err != nil {
		return ConversionArtifact{}, err
	}
	toolSet, err := agenttools.NewSet(agenttools.Config{
		Run: run, WorkerID: "evaluation", Client: sandbox, Channel: sandbox,
		Repository: sandbox, Now: func() time.Time { return scenario.Trigger.At },
		Timezone:  config.Timezone,
		Artifacts: sandbox,
		ArtifactDescriber: &artifact.LLMDescriber{
			Model: config.ArtifactModel, Vision: config.ArtifactVision,
		},
	})
	if err != nil {
		return ConversionArtifact{}, err
	}
	agent, err := agentruntime.NewFirstPartyAgent(
		ctx, run, scenario.Trigger.At.In(config.Timezone),
		config.Model, toolSet, sandbox, sandbox.CaptureArgumentsMiddleware(),
	)
	if err != nil {
		return ConversionArtifact{}, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent: agent, CheckPointStore: newMemoryCheckpointStore(),
	})
	var contextMessages []channel.ChannelMessage
	if commandKind == pactagent.CommandDiscussion {
		contextMessages = sandbox.InitialContext(channel.DefaultContextPageSize)
		if _, err := sandbox.AddContextMessages(
			ctx, run.ID, "evaluation", len(contextMessages), scenario.Trigger.At,
		); err != nil {
			return ConversionArtifact{}, err
		}
	}
	query, err := agentruntime.EncodeInitialQuery(
		scenario.Trigger.Text,
		nil,
		contextMessages,
		agentruntime.TriggerReference{
			ReplyToMessageID:    scenario.Trigger.ReplyToMessageID,
			ThreadRootMessageID: scenario.Trigger.ThreadRootMessageID,
		},
	)
	if err != nil {
		return ConversionArtifact{}, err
	}
	artifact := ConversionArtifact{
		Version: ArtifactVersion, ScenarioID: scenario.ID, RunID: run.ID,
		Model: config.ModelName, PromptVersion: agentruntime.PromptVersion,
		CommandKind: commandKind, GenerationStarted: startedAt.UTC(),
	}
	events := runner.Query(ctx, query, adk.WithCheckPointID(run.ID.String()))
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event == nil {
			artifact.GenerationError = "model emitted a nil event"
			break
		}
		if event.Err != nil {
			artifact.GenerationError = event.Err.Error()
			break
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			message, messageErr := event.Output.MessageOutput.GetMessage()
			if messageErr != nil {
				artifact.GenerationError = messageErr.Error()
				break
			}
			if message != nil && message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
				artifact.PromptTokens += message.ResponseMeta.Usage.PromptTokens
				artifact.CompletionTokens += message.ResponseMeta.Usage.CompletionTokens
				artifact.TotalTokens += message.ResponseMeta.Usage.TotalTokens
			}
		}
		if event.Action == nil || event.Action.Interrupted == nil {
			continue
		}
		for _, interrupted := range event.Action.Interrupted.InterruptContexts {
			if !interrupted.IsRootCause {
				continue
			}
			encoded, _ := json.Marshal(interrupted.Info)
			var clarification Clarification
			if json.Unmarshal(encoded, &clarification) == nil && clarification.Question != "" {
				artifact.Clarification = &clarification
				artifact.Outcome = "clarification"
			}
		}
	}
	if sandbox.CreatedTask != nil {
		request := sandbox.CreatedTask
		artifact.Task = &TaskProposal{
			Title: request.Title, Context: request.Context,
			ExpectedResult: request.ExpectedResult,
			ProjectNumber:  request.ProjectNumber,
			Priority:       string(request.Priority.Or("none")),
		}
		if value, ok := request.MilestoneID.Get(); ok {
			artifact.Task.MilestoneID = &value
		}
		if value, ok := request.AssigneeID.Get(); ok {
			artifact.Task.AssigneeID = &value
		}
		if value, ok := request.DueDate.Get(); ok {
			formatted := value.Format("2006-01-02")
			artifact.Task.DueDate = &formatted
		}
		artifact.Outcome = "task_created"
	}
	if response, ok := toolSet.Respond.LastResponse(); ok {
		artifact.Response = &ResponseArtifact{
			Type: response.Type, Summary: response.Summary, Message: response.Message,
		}
		if artifact.Outcome == "" {
			artifact.Outcome = response.Type
		}
	}
	if artifact.Outcome == "" {
		if artifact.GenerationError != "" {
			artifact.Outcome = "error"
		} else {
			artifact.Outcome = "no_terminal_selection"
		}
	}
	artifact.ToolTrace = sandbox.Traces()
	artifact.DurationMS = time.Since(startedAt).Milliseconds()
	return artifact, nil
}

type memoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryCheckpointStore() *memoryCheckpointStore {
	return &memoryCheckpointStore{data: make(map[string][]byte)}
}

func (s *memoryCheckpointStore) Get(
	_ context.Context,
	id string,
) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[id]
	return append([]byte(nil), value...), ok, nil
}

func (s *memoryCheckpointStore) Set(
	_ context.Context,
	id string,
	value []byte,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = append([]byte(nil), value...)
	return nil
}

func (s *memoryCheckpointStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (a ConversionArtifact) Validate() error {
	if a.Version != ArtifactVersion || a.ScenarioID == "" || a.RunID == uuid.Nil ||
		a.Model == "" || a.PromptVersion == "" || a.Outcome == "" {
		return fmt.Errorf("conversion artifact is incomplete")
	}
	if a.Task != nil && a.Outcome != "task_created" {
		return fmt.Errorf("conversion artifact Task does not match outcome")
	}
	return nil
}
