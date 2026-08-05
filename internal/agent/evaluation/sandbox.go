package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type ToolTrace struct {
	CallID        string          `json:"call_id"`
	ToolName      string          `json:"tool_name"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	State         string          `json:"state"`
	Result        json.RawMessage `json:"result,omitempty"`
	ErrorCategory string          `json:"error_category,omitempty"`
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
}

type Sandbox struct {
	mu                        sync.Mutex
	Scenario                  Scenario
	Run                       pactagent.Run
	CreatedTask               *generated.TaskCreate
	CreatedReceipt            *generated.Task
	contextMessages           []channel.ChannelMessage
	contextUsed               int
	toolCalls                 map[string]pactagent.ToolCall
	toolOrder                 []string
	toolArguments             map[string]json.RawMessage
	projects                  []generated.Project
	users                     []generated.User
	artifacts                 map[string]sandboxArtifact
	conversationConfiguration generated.AgentConversation
}

type sandboxArtifact struct {
	Reference artifact.Reference
	Fixture   string
	MessageID string
	CreatedAt time.Time
}

func NewSandbox(scenario Scenario, run pactagent.Run) (*Sandbox, error) {
	if err := scenario.Validate(); err != nil {
		return nil, err
	}
	creatorID := stableUUID(scenario.ID + ":creator")
	creator := generated.UserRef{ID: creatorID, Name: "Scenario Creator"}
	projects := make([]generated.Project, 0, len(scenario.Projects))
	for _, project := range scenario.Projects {
		projects = append(projects, generated.Project{
			ID:     stableUUID(fmt.Sprintf("%s:project:%d", scenario.ID, project.Number)),
			Number: project.Number, Version: 1, Name: project.Name,
			Creator:   creator,
			CreatedAt: scenario.Trigger.At.Add(-30 * 24 * time.Hour),
			UpdatedAt: scenario.Trigger.At.Add(-time.Hour),
		})
	}
	users := make([]generated.User, 0, len(scenario.Users))
	for _, user := range scenario.Users {
		users = append(users, generated.User{
			ID:   stableUUID(scenario.ID + ":user:" + user.Name),
			Name: user.Name, Active: true,
			PlatformRole: generated.UserPlatformRoleMEMBER,
		})
	}
	conversationConfiguration := generated.AgentConversation{
		ID:         stableUUID(scenario.ID + ":conversation"),
		Provider:   generated.AgentConversationProviderLark,
		ExternalID: "evaluation:" + scenario.ID,
		Enabled:    true,
		Version:    1,
		CanManage:  true,
		CreatedBy:  run.InitiatingUserID,
		UpdatedBy:  run.InitiatingUserID,
		LastSeenAt: scenario.Trigger.At,
		CreatedAt:  scenario.Trigger.At.Add(-time.Hour),
		UpdatedAt:  scenario.Trigger.At,
	}
	if configured := scenario.ConversationConfiguration; configured != nil {
		conversationConfiguration.BusinessContext = configured.BusinessContext
		for _, project := range projects {
			if project.Number == configured.DefaultProjectNumber {
				conversationConfiguration.BindingActive = true
				conversationConfiguration.DefaultProject = generated.NewOptProjectRef(
					generated.ProjectRef{ID: project.ID, Number: project.Number, Name: project.Name},
				)
				break
			}
		}
	}
	messages := make([]channel.ChannelMessage, 0, len(scenario.Messages))
	artifacts := make(map[string]sandboxArtifact)
	for _, message := range scenario.Messages {
		references := make([]artifact.Reference, 0, len(message.Artifacts))
		for _, attached := range message.Artifacts {
			reference := artifact.Reference{
				ID: attached.ID, Kind: attached.Kind, Name: attached.Name,
				MediaType:    attached.MediaType,
				Availability: artifact.AvailabilityAvailable,
			}
			references = append(references, reference)
			artifacts[attached.ID] = sandboxArtifact{
				Reference: reference, Fixture: attached.Fixture,
				MessageID: message.MessageID, CreatedAt: message.At.UTC(),
			}
		}
		messages = append(messages, channel.ChannelMessage{
			MessageID:       message.MessageID,
			SenderSubjectID: stableUUID(scenario.ID + ":sender:" + message.Sender).String(),
			SenderName:      message.Sender,
			Text:            message.Text,
			Artifacts:       references,
			CreatedAt:       message.At.UTC(),
		})
	}
	slices.SortFunc(messages, func(left, right channel.ChannelMessage) int {
		if left.CreatedAt.After(right.CreatedAt) {
			return -1
		}
		if left.CreatedAt.Before(right.CreatedAt) {
			return 1
		}
		return strings.Compare(left.MessageID, right.MessageID)
	})
	return &Sandbox{
		Scenario: scenario, Run: run,
		contextMessages: messages,
		toolCalls:       make(map[string]pactagent.ToolCall),
		toolArguments:   make(map[string]json.RawMessage),
		projects:        projects, users: users,
		artifacts:                 artifacts,
		conversationConfiguration: conversationConfiguration,
	}, nil
}

func stableUUID(value string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(value))
}

func (s *Sandbox) InitialContext(limit int) []channel.ChannelMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.contextMessages) {
		limit = len(s.contextMessages)
	}
	return append([]channel.ChannelMessage(nil), s.contextMessages[:limit]...)
}

func (s *Sandbox) Traces() []ToolTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	traces := make([]ToolTrace, 0, len(s.toolOrder))
	for _, callID := range s.toolOrder {
		call := s.toolCalls[callID]
		traces = append(traces, ToolTrace{
			CallID: call.ToolCallID, ToolName: call.ToolName,
			Arguments: append([]byte(nil), s.toolArguments[call.ToolCallID]...),
			State:     string(call.State), Result: append([]byte(nil), call.Result...),
			ErrorCategory: call.ErrorCategory, StartedAt: call.StartedAt,
			CompletedAt: call.CompletedAt,
		})
	}
	return traces
}

// CaptureArgumentsMiddleware records complete arguments only in the synthetic
// evaluation sandbox. Production Tool Ledger records hashes and byte counts so
// real conversation content is not retained.
func (s *Sandbox) CaptureArgumentsMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				if input != nil && input.CallID != "" && json.Valid([]byte(input.Arguments)) {
					s.mu.Lock()
					s.toolArguments[input.CallID] = append([]byte(nil), input.Arguments...)
					s.mu.Unlock()
				}
				return next(ctx, input)
			}
		},
	}
}

func (s *Sandbox) FetchContext(
	_ context.Context,
	request channel.ContextRequest,
) ([]channel.ChannelMessage, error) {
	if err := request.Validate(); err != nil ||
		request.TenantID != s.Run.TenantID ||
		request.ConversationID != s.Run.ConversationID ||
		request.TriggerMessageID != s.Run.TriggerMessageID {
		return nil, channel.ErrContextBoundary
	}
	offset := 0
	if request.BeforeCursor != "" {
		parsed, err := strconv.Atoi(request.BeforeCursor)
		if err != nil || parsed < 0 {
			return nil, channel.ErrContextBoundary
		}
		offset = parsed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset > len(s.contextMessages) {
		return nil, channel.ErrContextBoundary
	}
	end := min(offset+request.PageSize, len(s.contextMessages))
	page := append([]channel.ChannelMessage(nil), s.contextMessages[offset:end]...)
	if end < len(s.contextMessages) && len(page) > 0 {
		page[len(page)-1].Cursor = strconv.Itoa(end)
	}
	return page, nil
}

func (s *Sandbox) Reply(
	context.Context,
	channel.ReplyRequest,
) (channel.ProviderMessageID, error) {
	return "sandbox-reply", nil
}

func (s *Sandbox) Resolve(
	_ context.Context,
	scope artifact.Scope,
	artifactID string,
) (artifact.LocalFile, error) {
	if scope.RunID != s.Run.ID || scope.TenantID != s.Run.TenantID ||
		scope.ConversationID != s.Run.ConversationID {
		return artifact.LocalFile{}, artifact.ErrScope
	}
	s.mu.Lock()
	registered, ok := s.artifacts[artifactID]
	s.mu.Unlock()
	if !ok {
		return artifact.LocalFile{}, artifact.ErrNotFound
	}
	if registered.CreatedAt.Before(scope.NotBefore) || registered.CreatedAt.After(scope.NotAfter) {
		return artifact.LocalFile{}, artifact.ErrScope
	}
	path, err := materializeScenarioArtifact(registered)
	if err != nil {
		return artifact.LocalFile{}, err
	}
	return artifact.LocalFile{
		Reference: registered.Reference,
		Path:      path,
		Cleanup:   func() error { return os.Remove(path) },
	}, nil
}

func materializeScenarioArtifact(registered sandboxArtifact) (string, error) {
	encoded, err := scenarioFiles.ReadFile("testdata/artifacts/" + registered.Fixture)
	if err != nil {
		return "", fmt.Errorf("read scenario artifact fixture: %w", err)
	}
	extension := filepath.Ext(registered.Reference.Name)
	file, err := os.CreateTemp("", "pactline-evaluation-artifact-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create scenario artifact: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	var materializeErr error
	switch {
	case strings.HasSuffix(registered.Fixture, ".workbook.json"):
		materializeErr = materializeWorkbookFixture(path, encoded)
	case strings.HasSuffix(registered.Fixture, ".image.json"):
		materializeErr = materializeImageFixture(path, encoded)
	default:
		materializeErr = os.WriteFile(path, encoded, 0o600)
	}
	if materializeErr != nil {
		_ = os.Remove(path)
		return "", materializeErr
	}
	return path, nil
}

func materializeWorkbookFixture(path string, encoded []byte) error {
	var fixture struct {
		Sheets []struct {
			Name string     `json:"name"`
			Rows [][]string `json:"rows"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal(encoded, &fixture); err != nil || len(fixture.Sheets) == 0 {
		return errors.New("scenario workbook fixture is invalid")
	}
	book := excelize.NewFile()
	defer book.Close()
	defaultSheet := book.GetSheetName(0)
	for index, sheet := range fixture.Sheets {
		name := strings.TrimSpace(sheet.Name)
		if name == "" {
			return errors.New("scenario workbook sheet name is required")
		}
		if index == 0 {
			if err := book.SetSheetName(defaultSheet, name); err != nil {
				return err
			}
		} else if _, err := book.NewSheet(name); err != nil {
			return err
		}
		for rowIndex, row := range sheet.Rows {
			for columnIndex, value := range row {
				cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
				if err != nil {
					return err
				}
				if err := book.SetCellValue(name, cell, value); err != nil {
					return err
				}
			}
		}
	}
	if err := book.SaveAs(path); err != nil {
		return fmt.Errorf("write scenario workbook: %w", err)
	}
	return nil
}

func materializeImageFixture(path string, encoded []byte) error {
	var fixture struct {
		Width  int      `json:"width"`
		Height int      `json:"height"`
		Lines  []string `json:"lines"`
	}
	if err := json.Unmarshal(encoded, &fixture); err != nil || fixture.Width <= 0 || fixture.Height <= 0 {
		return errors.New("scenario image fixture is invalid")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, fixture.Width, fixture.Height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	drawer := font.Drawer{
		Dst: canvas, Src: image.NewUniform(color.RGBA{R: 25, G: 35, B: 45, A: 255}),
		Face: basicfont.Face7x13,
	}
	for index, line := range fixture.Lines {
		drawer.Dot = fixed.P(24, 35+index*34)
		drawer.DrawString(line)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, canvas)
	closeErr := file.Close()
	return errors.Join(encodeErr, closeErr)
}

func (s *Sandbox) ListProjects(
	context.Context,
	generated.ListProjectsParams,
) (generated.ListProjectsRes, error) {
	return &generated.ProjectListHeaders{
		Response: generated.ProjectList{Items: append([]generated.Project(nil), s.projects...)},
	}, nil
}

func (s *Sandbox) GetCurrentAgentConversationConfiguration(
	context.Context,
) (generated.GetCurrentAgentConversationConfigurationRes, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &generated.AgentConversationHeaders{
		Etag:     generated.NewOptString(fmt.Sprintf("\"%d\"", s.conversationConfiguration.Version)),
		Response: s.conversationConfiguration,
	}, nil
}

func (s *Sandbox) UpdateCurrentAgentConversationConfiguration(
	_ context.Context,
	request *generated.AgentConversationPatch,
	params generated.UpdateCurrentAgentConversationConfigurationParams,
) (generated.UpdateCurrentAgentConversationConfigurationRes, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if request == nil || params.IfMatch != fmt.Sprintf("\"%d\"", s.conversationConfiguration.Version) {
		return &generated.ProblemStatusCodeWithHeaders{StatusCode: 412}, nil
	}
	if value, ok := request.Enabled.Get(); ok {
		s.conversationConfiguration.Enabled = value
	}
	explicitBinding, bindingSet := request.BindingActive.Get()
	if bindingSet {
		s.conversationConfiguration.BindingActive = explicitBinding
	}
	if number, ok := request.DefaultProjectNumber.Get(); ok {
		matched := false
		for _, project := range s.projects {
			if project.Number == number {
				s.conversationConfiguration.DefaultProject = generated.NewOptProjectRef(
					generated.ProjectRef{ID: project.ID, Number: project.Number, Name: project.Name},
				)
				matched = true
				break
			}
		}
		if !matched {
			return &generated.ProblemStatusCodeWithHeaders{StatusCode: 404}, nil
		}
		if !bindingSet {
			s.conversationConfiguration.BindingActive = true
		}
	}
	if value, ok := request.BusinessContext.Get(); ok {
		s.conversationConfiguration.BusinessContext = value
	}
	s.conversationConfiguration.Version++
	s.conversationConfiguration.UpdatedAt = s.Scenario.Trigger.At
	return &generated.AgentConversationHeaders{
		Etag:     generated.NewOptString(fmt.Sprintf("\"%d\"", s.conversationConfiguration.Version)),
		Response: s.conversationConfiguration,
	}, nil
}

func (s *Sandbox) ListUsers(
	context.Context,
	generated.ListUsersParams,
) (generated.ListUsersRes, error) {
	return &generated.UserListHeaders{
		Response: generated.UserList{Items: append([]generated.User(nil), s.users...)},
	}, nil
}

func (s *Sandbox) ListTasks(
	context.Context,
	generated.ListTasksParams,
) (generated.ListTasksRes, error) {
	return &generated.TaskListHeaders{Response: generated.TaskList{}}, nil
}

func (s *Sandbox) GetTask(
	context.Context,
	generated.GetTaskParams,
) (generated.GetTaskRes, error) {
	return &generated.ProblemStatusCodeWithHeaders{StatusCode: 404}, nil
}

func (s *Sandbox) GetProject(
	_ context.Context,
	params generated.GetProjectParams,
) (generated.GetProjectRes, error) {
	for _, project := range s.projects {
		if project.Number == params.Number {
			return &generated.ProjectDetailHeaders{
				Response: generated.ProjectDetail{Project: project},
			}, nil
		}
	}
	return &generated.ProblemStatusCodeWithHeaders{StatusCode: 404}, nil
}

func (s *Sandbox) CreateTask(
	_ context.Context,
	request *generated.TaskCreate,
	_ generated.CreateTaskParams,
) (generated.CreateTaskRes, error) {
	if request == nil {
		return nil, errors.New("sandbox received a nil Task request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreatedTask != nil {
		return &generated.TaskCreatedHeaders{
			IdempotencyReplayed: generated.NewOptBool(true),
			Response:            *s.CreatedReceipt,
		}, nil
	}
	var project *generated.Project
	for index := range s.projects {
		if s.projects[index].Number == request.ProjectNumber {
			project = &s.projects[index]
			break
		}
	}
	if project == nil {
		return &generated.ProblemStatusCodeWithHeaders{StatusCode: 422}, nil
	}
	created := generated.Task{
		ID:     stableUUID(s.Scenario.ID + ":created-task"),
		Number: 9001, Version: 1,
		Title: request.Title, Context: request.Context,
		ExpectedResult: request.ExpectedResult,
		Status:         generated.TaskStatusTodo,
		Priority:       request.Priority.Or(generated.TaskPriorityNone),
		ExecutionMode:  generated.TaskExecutionModeHumanOnly,
		Creator:        generated.UserRef{ID: s.Run.InitiatingUserID, Name: s.Scenario.Trigger.Sender},
		Project:        generated.ProjectRef{ID: project.ID, Number: project.Number, Name: project.Name},
		CreatedAt:      s.Scenario.Trigger.At, UpdatedAt: s.Scenario.Trigger.At,
	}
	if assigneeID, ok := request.AssigneeID.Get(); ok {
		for _, user := range s.users {
			if user.ID == assigneeID {
				created.Assignee = generated.NewOptUserRef(generated.UserRef{
					ID: user.ID, Name: user.Name,
				})
				break
			}
		}
	}
	if dueDate, ok := request.DueDate.Get(); ok {
		created.DueDate = generated.NewOptDate(dueDate)
	}
	copyRequest := *request
	s.CreatedTask = &copyRequest
	s.CreatedReceipt = &created
	return &generated.TaskCreatedHeaders{Response: created}, nil
}

func (s *Sandbox) GetRun(context.Context, uuid.UUID) (pactagent.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Run, nil
}

func (s *Sandbox) GetCompletedToolCall(
	_ context.Context,
	_ uuid.UUID,
	toolCallID string,
) (pactagent.ToolCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call, ok := s.toolCalls[toolCallID]
	if !ok || call.State != pactagent.ToolCallCompleted {
		return pactagent.ToolCall{}, pactagent.ErrToolEvidenceNotFound
	}
	return call, nil
}

func (s *Sandbox) AddContextMessages(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	count int,
	_ time.Time,
) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count < 0 || s.contextUsed+count > pactagent.MaxContextMessages {
		return s.contextUsed, pactagent.ErrContextLimit
	}
	s.contextUsed += count
	return s.contextUsed, nil
}

func (s *Sandbox) AttachTask(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	taskID uuid.UUID,
	taskNumber int64,
	_ time.Time,
) (uuid.UUID, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Run.CreatedTaskID != nil {
		return *s.Run.CreatedTaskID, *s.Run.CreatedTaskNumber, false, nil
	}
	s.Run.CreatedTaskID = &taskID
	s.Run.CreatedTaskNumber = &taskNumber
	return taskID, taskNumber, true, nil
}

func (s *Sandbox) ClaimToolCall(
	_ context.Context,
	call pactagent.ToolCall,
) (pactagent.ToolCallClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.toolCalls[call.ToolCallID]; ok {
		if existing.ToolName != call.ToolName ||
			!bytes.Equal(existing.ArgumentHash, call.ArgumentHash) {
			return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimConflict}, nil
		}
		if existing.State == pactagent.ToolCallCompleted {
			return pactagent.ToolCallClaim{
				Kind:   pactagent.ToolCallClaimReplay,
				Result: append([]byte(nil), existing.Result...),
			}, nil
		}
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimRunning}, nil
	}
	s.toolCalls[call.ToolCallID] = call
	s.toolOrder = append(s.toolOrder, call.ToolCallID)
	return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimAcquired}, nil
}

func (s *Sandbox) CompleteToolCall(
	_ context.Context,
	_ uuid.UUID,
	toolCallID string,
	result []byte,
	now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	call, ok := s.toolCalls[toolCallID]
	if !ok {
		return pactagent.ErrToolCallProtocol
	}
	call.State = pactagent.ToolCallCompleted
	call.Result = append([]byte(nil), result...)
	completedAt := now.UTC()
	call.CompletedAt = &completedAt
	s.toolCalls[toolCallID] = call
	return nil
}

func (s *Sandbox) FailToolCall(
	_ context.Context,
	_ uuid.UUID,
	toolCallID string,
	category string,
	now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	call, ok := s.toolCalls[toolCallID]
	if !ok {
		return pactagent.ErrToolCallProtocol
	}
	call.State = pactagent.ToolCallFailed
	call.ErrorCategory = category
	completedAt := now.UTC()
	call.CompletedAt = &completedAt
	s.toolCalls[toolCallID] = call
	return nil
}

var _ channel.ChannelAdapter = (*Sandbox)(nil)
var _ artifact.Resolver = (*Sandbox)(nil)
