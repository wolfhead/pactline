package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	pactagent "github.com/wolfhead/pactline/internal/agent"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	ToolRespond = "respond"

	ResponseTaskCreated        = "task_created"
	ResponseTaskDetail         = "task_detail"
	ResponseProjectStatus      = "project_status"
	ResponseMilestoneStatus    = "milestone_status"
	ResponseConversationConfig = "conversation_configuration"
	ResponseError              = "error"
	ResponseAskUser            = "ask_user_question"
	ResponseGeneral            = "general_response"

	maxResponseSummaryLength = 1_000
	maxGeneralResponseLength = 4_000
)

type RespondInput struct {
	ResponseType      string   `json:"response_type" jsonschema:"required,enum=task_created,enum=task_detail,enum=project_status,enum=milestone_status,enum=conversation_configuration,enum=error,enum=ask_user_question,enum=general_response" jsonschema_description:"Platform response template to use"`
	SourceToolCallIDs []string `json:"source_tool_call_ids,omitempty" jsonschema_description:"Same-Run evidence IDs; must contain exactly one result from the business tool compatible with the selected template and may include supporting search evidence"`
	Summary           string   `json:"summary,omitempty" jsonschema_description:"Required concise Markdown interpretation for every structured business response; shown separately from verified fields"`
	Message           string   `json:"message,omitempty" jsonschema_description:"Bounded Markdown for error or general_response"`
	Question          string   `json:"question,omitempty" jsonschema_description:"Focused clarification question for ask_user_question"`
	Candidates        []string `json:"candidates,omitempty" jsonschema_description:"At most three concise clarification candidates"`
}

type ResponseSelection struct {
	Type                      string
	Summary                   string
	Message                   string
	CreatedTask               *CreatedTask
	TaskDetail                *TaskDetail
	ProjectOverview           *ProjectOverview
	MilestoneOverview         *MilestoneOverview
	ConversationConfiguration *ConversationConfigurationResult
}

type responseState struct {
	mu        sync.Mutex
	selection *ResponseSelection
}

func (s *responseState) ensureOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selection != nil {
		return fmt.Errorf("%w: terminal response was already selected", pactagent.ErrToolCallProtocol)
	}
	return nil
}

func (s *responseState) complete(selection ResponseSelection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selection != nil {
		return fmt.Errorf("%w: terminal response was already selected", pactagent.ErrToolCallProtocol)
	}
	s.selection = &selection
	return nil
}

func (s *responseState) last() (ResponseSelection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selection == nil {
		return ResponseSelection{}, false
	}
	return *s.selection, true
}

type RespondTool struct {
	config     Config
	state      *responseState
	createTask *CreateTaskTool
}

func (t *RespondTool) Info(context.Context) (*schema.ToolInfo, error) {
	return toolutils.GoStruct2ToolInfo[RespondInput](
		ToolRespond,
		"Select exactly one platform-owned terminal response. Structured business responses require compatible evidence IDs. Use ask_user_question to pause for clarification. Ordinary assistant prose is never sent.",
	)
}

func (t *RespondTool) InvokableRun(
	ctx context.Context,
	arguments string,
	_ ...tool.Option,
) (string, error) {
	var input RespondInput
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", fmt.Errorf("%w: decode respond: %w", ErrToolInput, err)
	}
	input.ResponseType = strings.TrimSpace(input.ResponseType)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Message = strings.TrimSpace(input.Message)
	if utf8.RuneCountInString(input.Summary) > maxResponseSummaryLength {
		return "", fmt.Errorf("%w: response summary is too long", ErrToolInput)
	}
	if requiresResponseSummary(input.ResponseType) && input.Summary == "" {
		return "", fmt.Errorf(
			"%w: %w: structured response requires a Markdown summary",
			ErrToolInput,
			ErrResponseSummary,
		)
	}
	if err := t.requireMutationReceipt(ctx, input.ResponseType); err != nil {
		return "", err
	}
	if input.ResponseType == ResponseAskUser {
		answer, err := askUser(ctx, AskUserInput{
			Question: input.Question, Candidates: input.Candidates,
		})
		if err != nil {
			return "", err
		}
		encoded, _ := json.Marshal(answer)
		return string(encoded), nil
	}
	if err := t.state.ensureOpen(); err != nil {
		return "", err
	}
	selection, err := t.selectResponse(ctx, input)
	if err != nil {
		return "", err
	}
	if err := t.state.complete(selection); err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(map[string]any{
		"accepted":             true,
		"response_type":        selection.Type,
		"source_tool_call_ids": input.SourceToolCallIDs,
	})
	return string(encoded), nil
}

func (t *RespondTool) LastResponse() (ResponseSelection, bool) {
	return t.state.last()
}

func (t *RespondTool) selectResponse(
	ctx context.Context,
	input RespondInput,
) (ResponseSelection, error) {
	selection := ResponseSelection{
		Type: input.ResponseType, Summary: input.Summary, Message: input.Message,
	}
	switch input.ResponseType {
	case ResponseTaskCreated:
		call, err := t.compatibleEvidence(ctx, input.SourceToolCallIDs, ToolCreateTask)
		if err != nil {
			created, ok := CreatedTask{}, false
			if t.createTask != nil {
				created, ok = t.createTask.LastCreated()
			}
			if !ok {
				return ResponseSelection{}, err
			}
			run, runErr := t.config.Repository.GetRun(ctx, t.config.Run.ID)
			if runErr != nil {
				return ResponseSelection{}, runErr
			}
			if run.CreatedTaskID == nil || run.CreatedTaskNumber == nil ||
				*run.CreatedTaskID != created.ID || *run.CreatedTaskNumber != created.Number {
				return ResponseSelection{}, err
			}
			slog.Warn("Agent task response recovered from local mutation receipt",
				"run_id", t.config.Run.ID, "evidence_error", err)
			selection.CreatedTask = &created
			break
		}
		var result CreatedTask
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Number <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid create_task evidence", pactagent.ErrToolCallProtocol)
		}
		run, err := t.config.Repository.GetRun(ctx, t.config.Run.ID)
		if err != nil {
			return ResponseSelection{}, err
		}
		if run.CreatedTaskNumber == nil || *run.CreatedTaskNumber != result.Number {
			return ResponseSelection{}, fmt.Errorf("%w: Task evidence is not attached to the Run", pactagent.ErrToolCallProtocol)
		}
		selection.CreatedTask = &result
	case ResponseTaskDetail:
		call, err := t.compatibleEvidence(ctx, input.SourceToolCallIDs, ToolGetTask)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result TaskDetail
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Number <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid get_task evidence", pactagent.ErrToolCallProtocol)
		}
		selection.TaskDetail = &result
	case ResponseProjectStatus:
		call, err := t.compatibleEvidence(ctx, input.SourceToolCallIDs, ToolGetProjectOverview)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result ProjectOverview
		if err := json.Unmarshal(call.Result, &result); err != nil || result.ProjectNumber <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid Project evidence", pactagent.ErrToolCallProtocol)
		}
		selection.ProjectOverview = &result
	case ResponseMilestoneStatus:
		call, err := t.compatibleEvidence(ctx, input.SourceToolCallIDs, ToolGetMilestoneOverview)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result MilestoneOverviewResult
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Overview == nil {
			return ResponseSelection{}, fmt.Errorf("%w: unresolved Milestone evidence", pactagent.ErrToolCallProtocol)
		}
		selection.MilestoneOverview = result.Overview
	case ResponseConversationConfig:
		call, err := t.compatibleEvidenceAny(
			ctx,
			input.SourceToolCallIDs,
			ToolGetConversationConfig,
			ToolUpdateConversationConfig,
		)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result ConversationConfigurationResult
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Version <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid conversation configuration evidence", pactagent.ErrToolCallProtocol)
		}
		selection.ConversationConfiguration = &result
	case ResponseError:
		if input.Message == "" || utf8.RuneCountInString(input.Message) > maxGeneralResponseLength {
			return ResponseSelection{}, fmt.Errorf("%w: error response message is invalid", ErrToolInput)
		}
	case ResponseGeneral:
		if input.Message == "" || utf8.RuneCountInString(input.Message) > maxGeneralResponseLength {
			return ResponseSelection{}, fmt.Errorf("%w: general response message is invalid", ErrToolInput)
		}
	default:
		return ResponseSelection{}, fmt.Errorf("%w: unsupported response type", ErrToolInput)
	}
	return selection, nil
}

func (t *RespondTool) requireMutationReceipt(
	ctx context.Context,
	responseType string,
) error {
	run, err := t.config.Repository.GetRun(ctx, t.config.Run.ID)
	if err != nil {
		return err
	}
	if run.CreatedTaskID != nil && responseType != ResponseTaskCreated {
		return fmt.Errorf(
			"%w: a created Task requires task_created response",
			pactagent.ErrToolCallProtocol,
		)
	}
	return nil
}

func (t *RespondTool) compatibleEvidence(
	ctx context.Context,
	toolCallIDs []string,
	expectedTool string,
) (pactagent.ToolCall, error) {
	if len(toolCallIDs) == 0 {
		return pactagent.ToolCall{}, fmt.Errorf(
			"%w: %w: at least one evidence ID is required",
			pactagent.ErrToolCallProtocol,
			ErrResponseEvidence,
		)
	}
	seen := make(map[string]struct{}, len(toolCallIDs))
	var compatible []pactagent.ToolCall
	for _, toolCallID := range toolCallIDs {
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return pactagent.ToolCall{}, fmt.Errorf(
				"%w: %w: evidence ID cannot be empty",
				pactagent.ErrToolCallProtocol,
				ErrResponseEvidence,
			)
		}
		if _, duplicate := seen[toolCallID]; duplicate {
			continue
		}
		seen[toolCallID] = struct{}{}
		call, err := t.config.Repository.GetCompletedToolCall(
			ctx, t.config.Run.ID, toolCallID,
		)
		if err != nil {
			return pactagent.ToolCall{}, err
		}
		if call.ToolName == expectedTool {
			compatible = append(compatible, call)
		}
	}
	if len(compatible) != 1 {
		return pactagent.ToolCall{}, fmt.Errorf(
			"%w: %w: %s response requires exactly one %s evidence result",
			pactagent.ErrToolCallProtocol,
			ErrResponseEvidence,
			inputResponseLabel(expectedTool),
			expectedTool,
		)
	}
	return compatible[0], nil
}

func (t *RespondTool) compatibleEvidenceAny(
	ctx context.Context,
	toolCallIDs []string,
	expectedTools ...string,
) (pactagent.ToolCall, error) {
	if len(toolCallIDs) == 0 {
		return pactagent.ToolCall{}, fmt.Errorf(
			"%w: %w: at least one evidence ID is required",
			pactagent.ErrToolCallProtocol,
			ErrResponseEvidence,
		)
	}
	seen := make(map[string]struct{}, len(toolCallIDs))
	var compatible []pactagent.ToolCall
	for _, toolCallID := range toolCallIDs {
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return pactagent.ToolCall{}, fmt.Errorf("%w: evidence ID cannot be empty", pactagent.ErrToolCallProtocol)
		}
		if _, duplicate := seen[toolCallID]; duplicate {
			continue
		}
		seen[toolCallID] = struct{}{}
		call, err := t.config.Repository.GetCompletedToolCall(ctx, t.config.Run.ID, toolCallID)
		if err != nil {
			return pactagent.ToolCall{}, err
		}
		if slices.Contains(expectedTools, call.ToolName) {
			compatible = append(compatible, call)
		}
	}
	if len(compatible) != 1 {
		return pactagent.ToolCall{}, fmt.Errorf(
			"%w: %w: conversation configuration response requires exactly one configuration evidence result",
			pactagent.ErrToolCallProtocol,
			ErrResponseEvidence,
		)
	}
	return compatible[0], nil
}

func inputResponseLabel(toolName string) string {
	return strings.TrimPrefix(toolName, "get_")
}

func requiresResponseSummary(responseType string) bool {
	switch responseType {
	case ResponseTaskCreated,
		ResponseTaskDetail,
		ResponseProjectStatus,
		ResponseMilestoneStatus,
		ResponseConversationConfig:
		return true
	default:
		return false
	}
}
