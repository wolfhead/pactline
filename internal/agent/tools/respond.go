package tools

import (
	"context"
	"encoding/json"
	"fmt"
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

	ResponseTaskCreated     = "task_created"
	ResponseTaskDetail      = "task_detail"
	ResponseProjectStatus   = "project_status"
	ResponseMilestoneStatus = "milestone_status"
	ResponseError           = "error"
	ResponseAskUser         = "ask_user_question"
	ResponseGeneral         = "general_response"

	maxResponseSummaryLength = 1_000
	maxGeneralResponseLength = 4_000
)

type RespondInput struct {
	ResponseType      string   `json:"response_type" jsonschema:"required,enum=task_created,enum=task_detail,enum=project_status,enum=milestone_status,enum=error,enum=ask_user_question,enum=general_response" jsonschema_description:"Platform response template to use"`
	SourceToolCallIDs []string `json:"source_tool_call_ids,omitempty" jsonschema_description:"Evidence IDs returned by compatible completed business tools in this Run"`
	Summary           string   `json:"summary,omitempty" jsonschema_description:"Optional Agent interpretation shown separately from verified fields"`
	Message           string   `json:"message,omitempty" jsonschema_description:"Plain text for error or general_response"`
	Question          string   `json:"question,omitempty" jsonschema_description:"Focused clarification question for ask_user_question"`
	Candidates        []string `json:"candidates,omitempty" jsonschema_description:"At most three concise clarification candidates"`
}

type ResponseSelection struct {
	Type              string
	Summary           string
	Message           string
	CreatedTask       *CreatedTask
	TaskDetail        *TaskDetail
	ProjectOverview   *ProjectOverview
	MilestoneOverview *MilestoneOverview
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
	config Config
	state  *responseState
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
		call, err := t.singleEvidence(ctx, input.SourceToolCallIDs, ToolCreateTask)
		if err != nil {
			return ResponseSelection{}, err
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
		call, err := t.singleEvidence(ctx, input.SourceToolCallIDs, ToolGetTask)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result TaskDetail
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Number <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid get_task evidence", pactagent.ErrToolCallProtocol)
		}
		selection.TaskDetail = &result
	case ResponseProjectStatus:
		call, err := t.singleEvidence(ctx, input.SourceToolCallIDs, ToolGetProjectOverview)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result ProjectOverview
		if err := json.Unmarshal(call.Result, &result); err != nil || result.ProjectNumber <= 0 {
			return ResponseSelection{}, fmt.Errorf("%w: invalid Project evidence", pactagent.ErrToolCallProtocol)
		}
		selection.ProjectOverview = &result
	case ResponseMilestoneStatus:
		call, err := t.singleEvidence(ctx, input.SourceToolCallIDs, ToolGetMilestoneOverview)
		if err != nil {
			return ResponseSelection{}, err
		}
		var result MilestoneOverviewResult
		if err := json.Unmarshal(call.Result, &result); err != nil || result.Overview == nil {
			return ResponseSelection{}, fmt.Errorf("%w: unresolved Milestone evidence", pactagent.ErrToolCallProtocol)
		}
		selection.MilestoneOverview = result.Overview
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

func (t *RespondTool) singleEvidence(
	ctx context.Context,
	toolCallIDs []string,
	expectedTool string,
) (pactagent.ToolCall, error) {
	if len(toolCallIDs) != 1 || strings.TrimSpace(toolCallIDs[0]) == "" {
		return pactagent.ToolCall{}, fmt.Errorf("%w: exactly one evidence ID is required", ErrToolInput)
	}
	call, err := t.config.Repository.GetCompletedToolCall(
		ctx, t.config.Run.ID, strings.TrimSpace(toolCallIDs[0]),
	)
	if err != nil {
		return pactagent.ToolCall{}, err
	}
	if call.ToolName != expectedTool {
		return pactagent.ToolCall{}, fmt.Errorf(
			"%w: %s response requires %s evidence",
			pactagent.ErrToolCallProtocol, inputResponseLabel(expectedTool), expectedTool,
		)
	}
	return call, nil
}

func inputResponseLabel(toolName string) string {
	return strings.TrimPrefix(toolName, "get_")
}
