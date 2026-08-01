// Package tools implements the bounded, model-visible capabilities of
// Pactline's first-party Agent. Business reads and writes use only the
// generated /api/v1 client.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	ToolGetConversationContext = "get_conversation_context"
	ToolInspectArtifact        = "inspect_artifact"
	ToolSearchProjects         = "search_projects"
	ToolSearchUsers            = "search_users"
	ToolAskUser                = "ask_user"
	ToolCreateTask             = "create_task"
	ToolSearchTasks            = "search_tasks"
	ToolGetTask                = "get_task"
	ToolGetProjectOverview     = "get_project_overview"
	ToolGetMilestoneOverview   = "get_milestone_overview"
)

var (
	ErrToolConfiguration = errors.New("Agent tool configuration is invalid")
	ErrToolInput         = errors.New("Agent tool input is invalid")
	ErrOpenAPI           = errors.New("Agent OpenAPI operation failed")
	ErrPermission        = errors.New("Agent operation was denied")
	ErrRetryable         = errors.New("Agent operation may be retried")
	ErrResponseEvidence  = errors.New("Agent response evidence is invalid")
	ErrResponseSummary   = errors.New("Agent response summary is missing")
)

type OpenAPIClient interface {
	ListProjects(context.Context, generated.ListProjectsParams) (generated.ListProjectsRes, error)
	ListUsers(context.Context, generated.ListUsersParams) (generated.ListUsersRes, error)
	ListTasks(context.Context, generated.ListTasksParams) (generated.ListTasksRes, error)
	GetTask(context.Context, generated.GetTaskParams) (generated.GetTaskRes, error)
	GetProject(context.Context, generated.GetProjectParams) (generated.GetProjectRes, error)
	CreateTask(context.Context, *generated.TaskCreate, generated.CreateTaskParams) (generated.CreateTaskRes, error)
}

type RunRepository interface {
	GetRun(context.Context, uuid.UUID) (pactagent.Run, error)
	GetCompletedToolCall(context.Context, uuid.UUID, string) (pactagent.ToolCall, error)
	AddContextMessages(context.Context, uuid.UUID, string, int, time.Time) (int, error)
	AttachTask(
		context.Context,
		uuid.UUID,
		string,
		uuid.UUID,
		int64,
		time.Time,
	) (uuid.UUID, int64, bool, error)
}

type Config struct {
	Run               pactagent.Run
	WorkerID          string
	Client            OpenAPIClient
	Channel           channel.ChannelAdapter
	Repository        RunRepository
	Now               func() time.Time
	Timezone          *time.Location
	Artifacts         artifact.Resolver
	ArtifactDescriber artifact.Describer
}

type Set struct {
	Tools      []tool.BaseTool
	CreateTask *CreateTaskTool
	Respond    *RespondTool
}

func NewSet(config Config) (Set, error) {
	if config.Run.ID == uuid.Nil || config.WorkerID == "" ||
		config.Client == nil || config.Repository == nil {
		return Set{}, ErrToolConfiguration
	}
	if (config.Artifacts == nil) != (config.ArtifactDescriber == nil) {
		return Set{}, ErrToolConfiguration
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Timezone == nil {
		config.Timezone = time.UTC
	}
	responseState := &responseState{}
	var tools []tool.BaseTool
	if config.Channel != nil {
		contextTool, err := toolutils.InferTool(
			ToolGetConversationContext,
			"Fetch an older bounded page from only the trigger conversation. Use only when the existing context is insufficient.",
			func(ctx context.Context, input GetConversationContextInput) (ConversationContextResult, error) {
				if err := responseState.ensureOpen(); err != nil {
					return ConversationContextResult{}, err
				}
				return getConversationContext(ctx, config, input)
			},
		)
		if err != nil {
			return Set{}, fmt.Errorf("configure conversation context tool: %w", err)
		}
		tools = append(tools, contextTool)
	}
	if config.Artifacts != nil {
		tools = append(tools, &InspectArtifactTool{
			config: config, responseState: responseState,
		})
	}
	projectTool, err := toolutils.InferTool(
		ToolSearchProjects,
		"Search active Pactline Projects. Use an empty query to list active Projects when the user did not name one. The result identifies the only active Project when one exists. Use a matching result for an explicitly named Project; otherwise use the only active Project. Ask the user when resolution is missing or ambiguous.",
		func(ctx context.Context, input SearchInput) (ProjectSearchResult, error) {
			if err := responseState.ensureOpen(); err != nil {
				return ProjectSearchResult{}, err
			}
			return searchProjects(ctx, config.Client, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure project search tool: %w", err)
	}
	userTool, err := toolutils.InferTool(
		ToolSearchUsers,
		"Search active Pactline users for an explicitly requested assignee. Multiple plausible users require clarification.",
		func(ctx context.Context, input SearchInput) (UserSearchResult, error) {
			if err := responseState.ensureOpen(); err != nil {
				return UserSearchResult{}, err
			}
			return searchUsers(ctx, config.Client, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure user search tool: %w", err)
	}
	taskSearchTool, err := toolutils.InferTool(
		ToolSearchTasks,
		"Search a bounded set of Pactline Tasks. Use it to resolve a Task number or answer filtered Task-list questions.",
		func(ctx context.Context, input TaskSearchInput) (TaskSearchResult, error) {
			if err := responseState.ensureOpen(); err != nil {
				return TaskSearchResult{}, err
			}
			return searchTasks(ctx, config.Client, config.Now(), config.Timezone, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure Task search tool: %w", err)
	}
	getTaskTool, err := toolutils.InferTool(
		ToolGetTask,
		"Get verified details for one Pactline Task number.",
		func(ctx context.Context, input GetTaskInput) (TaskDetail, error) {
			if err := responseState.ensureOpen(); err != nil {
				return TaskDetail{}, err
			}
			return getTask(ctx, config.Client, config.Now(), config.Timezone, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure Task detail tool: %w", err)
	}
	projectOverviewTool, err := toolutils.InferTool(
		ToolGetProjectOverview,
		"Get a deterministic Project status aggregate including Task counts, Backlog, Milestones, overdue and blocked work.",
		func(ctx context.Context, input ProjectOverviewInput) (ProjectOverview, error) {
			if err := responseState.ensureOpen(); err != nil {
				return ProjectOverview{}, err
			}
			return getProjectOverview(ctx, config.Client, config.Now(), config.Timezone, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure Project overview tool: %w", err)
	}
	milestoneOverviewTool, err := toolutils.InferTool(
		ToolGetMilestoneOverview,
		"Resolve and summarize one Milestone inside a Project. An unresolved result returns candidates and requires ask_user_question.",
		func(ctx context.Context, input MilestoneOverviewInput) (MilestoneOverviewResult, error) {
			if err := responseState.ensureOpen(); err != nil {
				return MilestoneOverviewResult{}, err
			}
			return getMilestoneOverview(ctx, config.Client, config.Now(), config.Timezone, input)
		},
	)
	if err != nil {
		return Set{}, fmt.Errorf("configure Milestone overview tool: %w", err)
	}
	createTask := &CreateTaskTool{config: config, responseState: responseState}
	respond := &RespondTool{config: config, state: responseState, createTask: createTask}
	tools = append(
		tools,
		projectTool,
		userTool,
		taskSearchTool,
		getTaskTool,
		projectOverviewTool,
		milestoneOverviewTool,
		createTask,
		respond,
	)
	return Set{Tools: tools, CreateTask: createTask, Respond: respond}, nil
}

type SearchInput struct {
	Query string `json:"query" jsonschema:"description=Exact or partial Project name; omit or use an empty string when the user did not name a Project"`
}

type ProjectCandidate struct {
	Number int64  `json:"number"`
	Name   string `json:"name"`
}

type ProjectSearchResult struct {
	Candidates        []ProjectCandidate `json:"candidates"`
	OnlyActiveProject *ProjectCandidate  `json:"only_active_project,omitempty"`
}

func searchProjects(
	ctx context.Context,
	client OpenAPIClient,
	input SearchInput,
) (ProjectSearchResult, error) {
	query := strings.TrimSpace(input.Query)
	response, err := client.ListProjects(ctx, generated.ListProjectsParams{
		Limit:    generated.NewOptInt(200),
		Archived: generated.NewOptListProjectsArchived(generated.ListProjectsArchivedExclude),
	})
	if err != nil {
		return ProjectSearchResult{}, fmt.Errorf("%w: list projects: %w", ErrRetryable, err)
	}
	list, ok := response.(*generated.ProjectListHeaders)
	if !ok {
		return ProjectSearchResult{}, openAPIResponseError(response)
	}
	var onlyActiveProject *ProjectCandidate
	if len(list.Response.Items) == 1 {
		onlyActiveProject = &ProjectCandidate{
			Number: list.Response.Items[0].Number,
			Name:   list.Response.Items[0].Name,
		}
	}
	normalized := strings.ToLower(query)
	candidates := make([]ProjectCandidate, 0, len(list.Response.Items))
	for _, project := range list.Response.Items {
		if normalized == "" ||
			strings.Contains(strings.ToLower(project.Name), normalized) {
			candidates = append(candidates, ProjectCandidate{
				Number: project.Number,
				Name:   project.Name,
			})
		}
	}
	slices.SortFunc(candidates, func(a, b ProjectCandidate) int {
		aExact := strings.EqualFold(a.Name, query)
		bExact := strings.EqualFold(b.Name, query)
		if aExact != bExact {
			if aExact {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	return ProjectSearchResult{
		Candidates:        candidates,
		OnlyActiveProject: onlyActiveProject,
	}, nil
}

type UserCandidate struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type UserSearchResult struct {
	Candidates []UserCandidate `json:"candidates"`
}

func searchUsers(
	ctx context.Context,
	client OpenAPIClient,
	input SearchInput,
) (UserSearchResult, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return UserSearchResult{}, fmt.Errorf("%w: user query is required", ErrToolInput)
	}
	response, err := client.ListUsers(ctx, generated.ListUsersParams{
		Limit: generated.NewOptInt(200),
	})
	if err != nil {
		return UserSearchResult{}, fmt.Errorf("%w: list users: %w", ErrRetryable, err)
	}
	list, ok := response.(*generated.UserListHeaders)
	if !ok {
		return UserSearchResult{}, openAPIResponseError(response)
	}
	normalized := strings.ToLower(query)
	var candidates []UserCandidate
	for _, user := range list.Response.Items {
		if user.Active && strings.Contains(strings.ToLower(user.Name), normalized) {
			candidates = append(candidates, UserCandidate{ID: user.ID, Name: user.Name})
		}
	}
	slices.SortFunc(candidates, func(a, b UserCandidate) int {
		aExact := strings.EqualFold(a.Name, query)
		bExact := strings.EqualFold(b.Name, query)
		if aExact != bExact {
			if aExact {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	return UserSearchResult{Candidates: candidates}, nil
}

type GetConversationContextInput struct {
	BeforeCursor string `json:"before_cursor" jsonschema:"description=Opaque cursor returned by the preceding context page; omit for the first older page; never pass a message ID"`
	PageSize     int    `json:"page_size" jsonschema:"required,description=Number of messages from 1 through 20"`
}

type InspectArtifactInput struct {
	ArtifactID   string `json:"artifact_id" jsonschema:"required,description=Opaque artifact ID from a bounded conversation message"`
	AnalysisGoal string `json:"analysis_goal" jsonschema:"required,description=Specific decision question the attachment description must answer"`
}

type InspectArtifactTool struct {
	config        Config
	responseState *responseState
}

func (t *InspectArtifactTool) Info(context.Context) (*schema.ToolInfo, error) {
	return toolutils.GoStruct2ToolInfo[InspectArtifactInput](
		ToolInspectArtifact,
		"Describe one decision-relevant conversation artifact once for a specific analysis_goal. The result is a one-shot LLM description of bounded attachment evidence, not raw parser output. Large CSV and XLSX results describe only bounded samples. Do not call this tool twice for the same artifact.",
	)
}

func (t *InspectArtifactTool) InvokableRun(
	ctx context.Context,
	arguments string,
	_ ...tool.Option,
) (string, error) {
	if err := t.responseState.ensureOpen(); err != nil {
		return "", err
	}
	var input InspectArtifactInput
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", fmt.Errorf("%w: decode inspect_artifact: %w", ErrToolInput, err)
	}
	description, err := inspectArtifact(ctx, t.config, input)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(description)
	if err != nil {
		return "", fmt.Errorf("encode artifact description: %w", err)
	}
	return string(encoded), nil
}

func inspectArtifact(
	ctx context.Context,
	config Config,
	input InspectArtifactInput,
) (string, error) {
	artifactID := strings.TrimSpace(input.ArtifactID)
	if artifactID == "" {
		return "", fmt.Errorf("%w: artifact_id is required", ErrToolInput)
	}
	analysisGoal := strings.TrimSpace(input.AnalysisGoal)
	if analysisGoal == "" {
		return "", fmt.Errorf("%w: analysis_goal is required", ErrToolInput)
	}
	local, err := config.Artifacts.Resolve(ctx, artifact.Scope{
		RunID:          config.Run.ID,
		TenantID:       config.Run.TenantID,
		ConversationID: config.Run.ConversationID,
		NotBefore:      config.Run.TriggerOccurredAt.Add(-channel.MaxContextAge),
		NotAfter:       config.Run.TriggerOccurredAt,
	}, artifactID)
	if err != nil {
		return "", fmt.Errorf("inspect conversation artifact: %w", err)
	}
	result, inspectErr := config.ArtifactDescriber.Describe(ctx, local, analysisGoal)
	if local.Cleanup != nil {
		if cleanupErr := local.Cleanup(); cleanupErr != nil {
			inspectErr = errors.Join(inspectErr, fmt.Errorf("clean conversation artifact: %w", cleanupErr))
		}
	}
	if inspectErr != nil {
		return "", inspectErr
	}
	return result, nil
}

type ConversationContextResult struct {
	Messages   []channel.ChannelMessage `json:"messages"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	Used       int                      `json:"used"`
	Limit      int                      `json:"limit"`
}

func getConversationContext(
	ctx context.Context,
	config Config,
	input GetConversationContextInput,
) (ConversationContextResult, error) {
	if input.PageSize <= 0 || input.PageSize > channel.MaxContextPageSize {
		return ConversationContextResult{}, fmt.Errorf("%w: page size must be between 1 and 20", ErrToolInput)
	}
	now := config.Now().UTC()
	request := channel.ContextRequest{
		TenantID:         config.Run.TenantID,
		ConversationID:   config.Run.ConversationID,
		TriggerMessageID: config.Run.TriggerMessageID,
		BeforeCursor:     strings.TrimSpace(input.BeforeCursor),
		PageSize:         input.PageSize,
		NotBefore:        config.Run.TriggerOccurredAt.Add(-channel.MaxContextAge),
		NotAfter:         config.Run.TriggerOccurredAt,
	}
	if err := request.Validate(); err != nil {
		return ConversationContextResult{}, err
	}
	messages, err := config.Channel.FetchContext(ctx, request)
	if err != nil {
		return ConversationContextResult{}, err
	}
	if len(messages) > input.PageSize {
		return ConversationContextResult{}, channel.ErrContextBoundary
	}
	for _, message := range messages {
		if message.CreatedAt.Before(request.NotBefore) || !message.CreatedAt.Before(request.NotAfter) {
			return ConversationContextResult{}, channel.ErrContextBoundary
		}
	}
	used, err := config.Repository.AddContextMessages(
		ctx, config.Run.ID, config.WorkerID, len(messages), now,
	)
	if err != nil {
		return ConversationContextResult{}, err
	}
	nextCursor := ""
	if len(messages) == input.PageSize {
		nextCursor = messages[len(messages)-1].Cursor
	}
	return ConversationContextResult{
		Messages: messages, NextCursor: nextCursor,
		Used: used, Limit: pactagent.MaxContextMessages,
	}, nil
}

type AskUserInput struct {
	Question   string   `json:"question" jsonschema:"required,description=Focused clarification question"`
	Candidates []string `json:"candidates" jsonschema:"description=At most three concise candidates"`
}

type ClarificationState struct {
	Question string
}

type ClarificationInfo struct {
	Question   string   `json:"question"`
	Candidates []string `json:"candidates,omitempty"`
}

type AskUserResult struct {
	Answer string `json:"answer"`
}

func init() {
	schema.RegisterName[ClarificationState]("pactline_agent_clarification_state_v1")
	schema.RegisterName[ClarificationInfo]("pactline_agent_clarification_info_v1")
}

func askUser(ctx context.Context, input AskUserInput) (AskUserResult, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" || utf8.RuneCountInString(question) > 500 || len(input.Candidates) > 3 {
		return AskUserResult{}, fmt.Errorf("%w: invalid clarification question", ErrToolInput)
	}
	for index, candidate := range input.Candidates {
		input.Candidates[index] = strings.TrimSpace(candidate)
		if input.Candidates[index] == "" || utf8.RuneCountInString(input.Candidates[index]) > 200 {
			return AskUserResult{}, fmt.Errorf("%w: invalid clarification candidate", ErrToolInput)
		}
	}
	wasInterrupted, hasState, state := tool.GetInterruptState[ClarificationState](ctx)
	if !wasInterrupted {
		return AskUserResult{}, tool.StatefulInterrupt(
			ctx,
			ClarificationInfo{
				Question:   question,
				Candidates: input.Candidates,
			},
			ClarificationState{Question: question},
		)
	}
	if !hasState || state.Question == "" {
		return AskUserResult{}, fmt.Errorf("%w: clarification state is missing", ErrToolInput)
	}
	isTarget, hasData, answer := tool.GetResumeContext[string](ctx)
	if !isTarget {
		return AskUserResult{}, tool.StatefulInterrupt(
			ctx,
			ClarificationInfo{Question: state.Question},
			state,
		)
	}
	answer = strings.TrimSpace(answer)
	if !hasData || answer == "" {
		return AskUserResult{}, fmt.Errorf("%w: clarification answer is missing", ErrToolInput)
	}
	return AskUserResult{Answer: answer}, nil
}

type CreateTaskInput struct {
	Title          string  `json:"title" jsonschema:"required,description=Concise Task title"`
	Context        string  `json:"context" jsonschema:"required,description=Why the Task is needed"`
	ExpectedResult string  `json:"expected_result" jsonschema:"required,description=Observable result expected at completion"`
	ProjectNumber  int64   `json:"project_number" jsonschema:"required,description=Resolved Pactline Project number"`
	MilestoneID    *string `json:"milestone_id" jsonschema:"description=Resolved milestone UUID string or null for Project Backlog"`
	AssigneeID     *string `json:"assignee_id" jsonschema:"description=Resolved user UUID string or null when unassigned"`
	DueDate        *string `json:"due_date" jsonschema:"description=Unambiguous YYYY-MM-DD date or null"`
	Priority       string  `json:"priority" jsonschema:"required,description=One of none low medium high urgent; use none unless the conversation explicitly assigns priority"`
}

// UnmarshalJSON accepts an empty array as null for nullable scalar IDs. Some
// tool-calling models emit [] for an unset optional field even when the schema
// says UUID or null. Non-empty arrays and all other invalid shapes still fail.
func (i *CreateTaskInput) UnmarshalJSON(encoded []byte) error {
	type plainCreateTaskInput CreateTaskInput
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		if err != nil {
			return err
		}
		return errors.New("create_task input must be a JSON object")
	}
	for _, field := range []string{"milestone_id", "assignee_id"} {
		value := bytes.TrimSpace(object[field])
		if bytes.Equal(value, []byte("[]")) {
			object[field] = json.RawMessage("null")
			continue
		}
		if len(value) > 0 && value[0] == '[' {
			var octets []int
			if err := json.Unmarshal(value, &octets); err != nil || len(octets) != len(uuid.UUID{}) {
				return fmt.Errorf("%s must be a UUID string or null", field)
			}
			var id uuid.UUID
			for index, octet := range octets {
				if octet < 0 || octet > 255 {
					return fmt.Errorf("%s must be a UUID string or null", field)
				}
				id[index] = byte(octet)
			}
			encodedID, _ := json.Marshal(id.String())
			object[field] = encodedID
		}
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return err
	}
	var decoded plainCreateTaskInput
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		return err
	}
	*i = CreateTaskInput(decoded)
	return nil
}

type CreatedTask struct {
	ID               uuid.UUID `json:"id"`
	Number           int64     `json:"number"`
	Title            string    `json:"title"`
	ProjectNumber    int64     `json:"project_number"`
	ProjectName      string    `json:"project_name"`
	MilestoneName    string    `json:"milestone_name,omitempty"`
	AssigneeName     string    `json:"assignee_name,omitempty"`
	DueDate          *string   `json:"due_date,omitempty"`
	Status           string    `json:"status"`
	Priority         string    `json:"priority"`
	IdempotentReplay bool      `json:"idempotent_replay"`
}

type CreateTaskTool struct {
	config        Config
	responseState *responseState
	mu            sync.Mutex
	last          *CreatedTask
}

func (t *CreateTaskTool) Info(context.Context) (*schema.ToolInfo, error) {
	return toolutils.GoStruct2ToolInfo[CreateTaskInput](
		ToolCreateTask,
		"Create exactly one Pactline Task after Project and optional assignee are resolved. This is the only mutation tool.",
	)
}

func (t *CreateTaskTool) InvokableRun(
	ctx context.Context,
	arguments string,
	_ ...tool.Option,
) (string, error) {
	var input CreateTaskInput
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", fmt.Errorf("%w: decode create_task: %w", ErrToolInput, err)
	}
	result, err := t.create(ctx, input)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode create_task result: %w", err)
	}
	return string(encoded), nil
}

func (t *CreateTaskTool) LastCreated() (CreatedTask, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		return CreatedTask{}, false
	}
	return *t.last, true
}

func (t *CreateTaskTool) create(ctx context.Context, input CreateTaskInput) (CreatedTask, error) {
	if t.responseState != nil {
		if err := t.responseState.ensureOpen(); err != nil {
			return CreatedTask{}, err
		}
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Context = strings.TrimSpace(input.Context)
	input.ExpectedResult = strings.TrimSpace(input.ExpectedResult)
	if input.Title == "" || input.Context == "" || input.ExpectedResult == "" ||
		input.ProjectNumber <= 0 {
		return CreatedTask{}, fmt.Errorf("%w: title, context, expected result, and Project are required", ErrToolInput)
	}
	priority, err := taskPriority(input.Priority)
	if err != nil {
		return CreatedTask{}, err
	}
	request := &generated.TaskCreate{
		Title:          input.Title,
		Context:        input.Context,
		ExpectedResult: input.ExpectedResult,
		Priority:       generated.NewOptTaskPriority(priority),
		ProjectNumber:  input.ProjectNumber,
	}
	if input.MilestoneID != nil {
		milestoneID, present, parseErr := parseOptionalUUID(*input.MilestoneID)
		if parseErr != nil {
			return CreatedTask{}, fmt.Errorf("%w: milestone_id must be a UUID", ErrToolInput)
		}
		if present {
			request.MilestoneID = generated.NewOptNilUUID(milestoneID)
		}
	}
	if input.AssigneeID != nil {
		assigneeID, present, parseErr := parseOptionalUUID(*input.AssigneeID)
		if parseErr != nil {
			return CreatedTask{}, fmt.Errorf("%w: assignee_id must be a UUID", ErrToolInput)
		}
		if present {
			request.AssigneeID = generated.NewOptNilUUID(assigneeID)
		}
	}
	if input.DueDate != nil {
		dueDate, present, parseErr := parseOptionalDueDate(*input.DueDate)
		if parseErr != nil {
			return CreatedTask{}, fmt.Errorf("%w: due_date must use YYYY-MM-DD", ErrToolInput)
		}
		if present {
			request.DueDate = generated.NewOptNilDate(dueDate)
		}
	}
	response, err := t.config.Client.CreateTask(ctx, request, generated.CreateTaskParams{
		IdempotencyKey: generated.NewOptString(
			pactagent.CreateTaskIdempotencyKey(t.config.Run.ID),
		),
	})
	if err != nil {
		return CreatedTask{}, fmt.Errorf("%w: create task: %w", ErrRetryable, err)
	}
	created, ok := response.(*generated.TaskCreatedHeaders)
	if !ok {
		return CreatedTask{}, openAPIResponseError(response)
	}
	taskResult := createdTaskResult(created)
	existingID, existingNumber, attached, err := t.config.Repository.AttachTask(
		ctx,
		t.config.Run.ID,
		t.config.WorkerID,
		taskResult.ID,
		taskResult.Number,
		t.config.Now().UTC(),
	)
	if err != nil {
		return CreatedTask{}, err
	}
	if existingID != taskResult.ID || existingNumber != taskResult.Number {
		return CreatedTask{}, pactagent.ErrTaskAlreadyCreated
	}
	taskResult.IdempotentReplay = !attached ||
		created.IdempotencyReplayed.Or(false)
	t.mu.Lock()
	t.last = &taskResult
	t.mu.Unlock()
	return taskResult, nil
}

func parseOptionalDueDate(value string) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "null") {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

func parseOptionalUUID(value string) (uuid.UUID, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "null") {
		return uuid.Nil, false, nil
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false, errors.New("invalid UUID")
	}
	return id, true, nil
}

func taskPriority(value string) (generated.TaskPriority, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none":
		return generated.TaskPriorityNone, nil
	case "low":
		return generated.TaskPriorityLow, nil
	case "medium":
		return generated.TaskPriorityMedium, nil
	case "high":
		return generated.TaskPriorityHigh, nil
	case "urgent":
		return generated.TaskPriorityUrgent, nil
	default:
		return "", fmt.Errorf("%w: invalid priority", ErrToolInput)
	}
}

func createdTaskResult(response *generated.TaskCreatedHeaders) CreatedTask {
	task := response.Response
	result := CreatedTask{
		ID:            task.ID,
		Number:        task.Number,
		Title:         task.Title,
		ProjectNumber: task.Project.Number,
		ProjectName:   task.Project.Name,
		Status:        string(task.Status),
		Priority:      string(task.Priority),
	}
	if milestone, ok := task.Milestone.Get(); ok {
		result.MilestoneName = milestone.Name
	}
	if assignee, ok := task.Assignee.Get(); ok {
		result.AssigneeName = assignee.Name
	}
	if dueDate, ok := task.DueDate.Get(); ok {
		value := dueDate.Format("2006-01-02")
		result.DueDate = &value
	}
	return result
}

func openAPIResponseError(response any) error {
	problem, ok := response.(*generated.ProblemStatusCodeWithHeaders)
	if !ok {
		return fmt.Errorf("%w: unexpected response %T", ErrOpenAPI, response)
	}
	switch problem.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrPermission, problem.Response.Code)
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", ErrRetryable, problem.Response.Code)
	default:
		return fmt.Errorf("%w: status=%d code=%s", ErrOpenAPI, problem.StatusCode, problem.Response.Code)
	}
}

var _ tool.InvokableTool = (*CreateTaskTool)(nil)
