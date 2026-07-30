package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
)

const (
	defaultTaskSearchLimit = 10
	maxTaskSearchLimit     = 20
	maxAttentionTasks      = 5
	maxMilestoneSummaries  = 10
)

type TaskSearchInput struct {
	Query         string   `json:"query,omitempty" jsonschema_description:"Exact or partial Task title or text"`
	ProjectNumber *int64   `json:"project_number,omitempty" jsonschema_description:"Optional resolved Project number"`
	MilestoneID   *string  `json:"milestone_id,omitempty" jsonschema_description:"Optional resolved Milestone UUID"`
	Statuses      []string `json:"statuses,omitempty" jsonschema_description:"Optional Task statuses: todo, in_progress, in_review, done, cancelled"`
	Limit         int      `json:"limit,omitempty" jsonschema_description:"Maximum results from 1 through 20; defaults to 10"`
}

type TaskSummary struct {
	Number        int64   `json:"number"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	Priority      string  `json:"priority"`
	ProjectNumber int64   `json:"project_number"`
	ProjectName   string  `json:"project_name"`
	MilestoneName string  `json:"milestone_name,omitempty"`
	AssigneeName  string  `json:"assignee_name,omitempty"`
	DueDate       *string `json:"due_date,omitempty"`
	Blocked       bool    `json:"blocked"`
	Overdue       bool    `json:"overdue"`
}

type TaskSearchResult struct {
	Items     []TaskSummary `json:"items"`
	Truncated bool          `json:"truncated"`
}

func searchTasks(
	ctx context.Context,
	client OpenAPIClient,
	now time.Time,
	timezone *time.Location,
	input TaskSearchInput,
) (TaskSearchResult, error) {
	limit := input.Limit
	if limit == 0 {
		limit = defaultTaskSearchLimit
	}
	if limit < 1 || limit > maxTaskSearchLimit {
		return TaskSearchResult{}, fmt.Errorf("%w: Task search limit must be between 1 and 20", ErrToolInput)
	}
	params := generated.ListTasksParams{
		Limit:    generated.NewOptInt(limit + 1),
		Archived: generated.NewOptListTasksArchived(generated.ListTasksArchivedExclude),
		Sort:     generated.NewOptListTasksSort(generated.ListTasksSortUpdatedAt),
		Order:    generated.NewOptListTasksOrder(generated.ListTasksOrderDesc),
	}
	if query := strings.TrimSpace(input.Query); query != "" {
		params.Q = generated.NewOptString(query)
	}
	if input.ProjectNumber != nil {
		if *input.ProjectNumber <= 0 {
			return TaskSearchResult{}, fmt.Errorf("%w: Project number must be positive", ErrToolInput)
		}
		params.ProjectNumber = generated.NewOptInt64(*input.ProjectNumber)
	}
	if input.MilestoneID != nil {
		id, err := uuid.Parse(strings.TrimSpace(*input.MilestoneID))
		if err != nil || id == uuid.Nil {
			return TaskSearchResult{}, fmt.Errorf("%w: Milestone ID must be a UUID", ErrToolInput)
		}
		params.MilestoneID = generated.NewOptUUID(id)
	}
	for _, status := range input.Statuses {
		parsed, err := taskStatus(status)
		if err != nil {
			return TaskSearchResult{}, err
		}
		params.Status = append(params.Status, parsed)
	}
	response, err := client.ListTasks(ctx, params)
	if err != nil {
		return TaskSearchResult{}, fmt.Errorf("%w: list Tasks: %w", ErrRetryable, err)
	}
	list, ok := response.(*generated.TaskListHeaders)
	if !ok {
		return TaskSearchResult{}, openAPIResponseError(response)
	}
	truncated := len(list.Response.Items) > limit
	items := list.Response.Items
	if truncated {
		items = items[:limit]
	}
	result := TaskSearchResult{
		Items:     make([]TaskSummary, 0, len(items)),
		Truncated: truncated,
	}
	for _, task := range items {
		result.Items = append(result.Items, taskSummary(task, now, timezone))
	}
	return result, nil
}

type GetTaskInput struct {
	Number int64 `json:"number" jsonschema:"required" jsonschema_description:"Pactline Task number"`
}

type TaskDetail struct {
	TaskSummary
	Context             string  `json:"context,omitempty"`
	ExpectedResult      string  `json:"expected_result,omitempty"`
	StartDate           *string `json:"start_date,omitempty"`
	DependencyCount     int     `json:"dependency_count"`
	DependentCount      int     `json:"dependent_count"`
	ActiveChildCount    int     `json:"active_child_count"`
	CompletedChildCount int     `json:"completed_child_count"`
}

func getTask(
	ctx context.Context,
	client OpenAPIClient,
	now time.Time,
	timezone *time.Location,
	input GetTaskInput,
) (TaskDetail, error) {
	if input.Number <= 0 {
		return TaskDetail{}, fmt.Errorf("%w: Task number must be positive", ErrToolInput)
	}
	response, err := client.GetTask(ctx, generated.GetTaskParams{Number: input.Number})
	if err != nil {
		return TaskDetail{}, fmt.Errorf("%w: get Task: %w", ErrRetryable, err)
	}
	detail, ok := response.(*generated.TaskHeaders)
	if !ok {
		return TaskDetail{}, openAPIResponseError(response)
	}
	task := detail.Response
	result := TaskDetail{
		TaskSummary:     taskSummary(task, now, timezone),
		Context:         task.Context,
		ExpectedResult:  task.ExpectedResult,
		DependencyCount: len(task.Dependencies),
		DependentCount:  len(task.Dependents),
	}
	if value, ok := task.StartDate.Get(); ok {
		formatted := value.Format("2006-01-02")
		result.StartDate = &formatted
	}
	for _, child := range task.Children {
		if child.Status == generated.TaskStatusDone || child.Status == generated.TaskStatusCancelled {
			result.CompletedChildCount++
		} else {
			result.ActiveChildCount++
		}
	}
	return result, nil
}

type TaskStatusCounts struct {
	Todo       int `json:"todo"`
	InProgress int `json:"in_progress"`
	InReview   int `json:"in_review"`
	Done       int `json:"done"`
	Cancelled  int `json:"cancelled"`
}

type MilestoneSummary struct {
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	Status          string           `json:"status"`
	TargetDate      *string          `json:"target_date,omitempty"`
	TaskCount       int              `json:"task_count"`
	StatusCounts    TaskStatusCounts `json:"status_counts"`
	OverdueCount    int              `json:"overdue_count"`
	BlockedCount    int              `json:"blocked_count"`
	CompletionRatio float64          `json:"completion_ratio"`
}

type ProjectOverviewInput struct {
	ProjectNumber int64 `json:"project_number" jsonschema:"required" jsonschema_description:"Resolved Pactline Project number"`
}

type ProjectOverview struct {
	ProjectNumber       int64              `json:"project_number"`
	ProjectName         string             `json:"project_name"`
	OwnerName           string             `json:"owner_name"`
	Archived            bool               `json:"archived"`
	TaskCount           int                `json:"task_count"`
	StatusCounts        TaskStatusCounts   `json:"status_counts"`
	BacklogCount        int                `json:"backlog_count"`
	OverdueCount        int                `json:"overdue_count"`
	BlockedCount        int                `json:"blocked_count"`
	Milestones          []MilestoneSummary `json:"milestones"`
	MilestonesTruncated bool               `json:"milestones_truncated"`
	AttentionTasks      []TaskSummary      `json:"attention_tasks"`
}

func getProjectOverview(
	ctx context.Context,
	client OpenAPIClient,
	now time.Time,
	timezone *time.Location,
	input ProjectOverviewInput,
) (ProjectOverview, error) {
	if input.ProjectNumber <= 0 {
		return ProjectOverview{}, fmt.Errorf("%w: Project number must be positive", ErrToolInput)
	}
	detail, err := getProjectDetail(ctx, client, input.ProjectNumber)
	if err != nil {
		return ProjectOverview{}, err
	}
	return projectOverview(detail, now, timezone), nil
}

type MilestoneOverviewInput struct {
	ProjectNumber int64  `json:"project_number" jsonschema:"required" jsonschema_description:"Resolved Pactline Project number"`
	Query         string `json:"query,omitempty" jsonschema_description:"Exact or partial Milestone name, or Milestone UUID; omit only when the Project has one non-cancelled Milestone"`
}

type MilestoneCandidate struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

type MilestoneOverview struct {
	ProjectNumber  int64            `json:"project_number"`
	ProjectName    string           `json:"project_name"`
	Milestone      MilestoneSummary `json:"milestone"`
	Outcome        string           `json:"outcome,omitempty"`
	AttentionTasks []TaskSummary    `json:"attention_tasks"`
}

type MilestoneOverviewResult struct {
	Overview   *MilestoneOverview   `json:"overview,omitempty"`
	Candidates []MilestoneCandidate `json:"candidates,omitempty"`
}

func getMilestoneOverview(
	ctx context.Context,
	client OpenAPIClient,
	now time.Time,
	timezone *time.Location,
	input MilestoneOverviewInput,
) (MilestoneOverviewResult, error) {
	if input.ProjectNumber <= 0 {
		return MilestoneOverviewResult{}, fmt.Errorf("%w: Project number must be positive", ErrToolInput)
	}
	detail, err := getProjectDetail(ctx, client, input.ProjectNumber)
	if err != nil {
		return MilestoneOverviewResult{}, err
	}
	matches := resolveMilestones(detail.Milestones, strings.TrimSpace(input.Query))
	if len(matches) != 1 {
		candidates := make([]MilestoneCandidate, 0, min(len(matches), 5))
		for _, milestone := range matches {
			if len(candidates) == 5 {
				break
			}
			candidates = append(candidates, MilestoneCandidate{
				ID: milestone.ID, Name: milestone.Name, Status: string(milestone.Status),
			})
		}
		return MilestoneOverviewResult{Candidates: candidates}, nil
	}
	milestone := matches[0]
	tasks := make([]generated.Task, 0)
	for _, task := range detail.Tasks {
		if task.ArchivedAt.IsSet() {
			continue
		}
		ref, ok := task.Milestone.Get()
		if ok && ref.ID == milestone.ID {
			tasks = append(tasks, task)
		}
	}
	summary := milestoneSummary(milestone, tasks, now, timezone)
	overview := &MilestoneOverview{
		ProjectNumber:  detail.Project.Number,
		ProjectName:    detail.Project.Name,
		Milestone:      summary,
		Outcome:        milestone.Outcome,
		AttentionTasks: attentionTasks(tasks, now, timezone),
	}
	return MilestoneOverviewResult{Overview: overview}, nil
}

func getProjectDetail(
	ctx context.Context,
	client OpenAPIClient,
	projectNumber int64,
) (generated.ProjectDetail, error) {
	response, err := client.GetProject(ctx, generated.GetProjectParams{Number: projectNumber})
	if err != nil {
		return generated.ProjectDetail{}, fmt.Errorf("%w: get Project: %w", ErrRetryable, err)
	}
	detail, ok := response.(*generated.ProjectDetailHeaders)
	if !ok {
		return generated.ProjectDetail{}, openAPIResponseError(response)
	}
	return detail.Response, nil
}

func projectOverview(
	detail generated.ProjectDetail,
	now time.Time,
	timezone *time.Location,
) ProjectOverview {
	result := ProjectOverview{
		ProjectNumber: detail.Project.Number,
		ProjectName:   detail.Project.Name,
		OwnerName:     detail.Project.Owner.Name,
		Archived:      detail.Project.ArchivedAt.IsSet(),
		Milestones:    make([]MilestoneSummary, 0, len(detail.Milestones)),
	}
	milestoneTasks := make(map[uuid.UUID][]generated.Task, len(detail.Milestones))
	for _, task := range detail.Tasks {
		if task.ArchivedAt.IsSet() {
			continue
		}
		result.TaskCount++
		addTaskStatus(&result.StatusCounts, task.Status)
		if task.Blocked {
			result.BlockedCount++
		}
		if taskIsOverdue(task, now, timezone) {
			result.OverdueCount++
		}
		if milestone, ok := task.Milestone.Get(); ok {
			milestoneTasks[milestone.ID] = append(milestoneTasks[milestone.ID], task)
		} else {
			result.BacklogCount++
		}
	}
	for _, milestone := range detail.Milestones {
		result.Milestones = append(
			result.Milestones,
			milestoneSummary(milestone, milestoneTasks[milestone.ID], now, timezone),
		)
	}
	slices.SortFunc(result.Milestones, func(a, b MilestoneSummary) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(result.Milestones) > maxMilestoneSummaries {
		result.Milestones = result.Milestones[:maxMilestoneSummaries]
		result.MilestonesTruncated = true
	}
	result.AttentionTasks = attentionTasks(detail.Tasks, now, timezone)
	return result
}

func milestoneSummary(
	milestone generated.Milestone,
	tasks []generated.Task,
	now time.Time,
	timezone *time.Location,
) MilestoneSummary {
	result := MilestoneSummary{
		ID: milestone.ID, Name: milestone.Name, Status: string(milestone.Status),
		TaskCount: len(tasks),
	}
	if target, ok := milestone.TargetDate.Get(); ok {
		formatted := target.Format("2006-01-02")
		result.TargetDate = &formatted
	}
	for _, task := range tasks {
		addTaskStatus(&result.StatusCounts, task.Status)
		if task.Blocked {
			result.BlockedCount++
		}
		if taskIsOverdue(task, now, timezone) {
			result.OverdueCount++
		}
	}
	eligible := result.TaskCount - result.StatusCounts.Cancelled
	if eligible > 0 {
		result.CompletionRatio = float64(result.StatusCounts.Done) / float64(eligible)
	}
	return result
}

func resolveMilestones(
	milestones []generated.Milestone,
	query string,
) []generated.Milestone {
	if query == "" {
		var active []generated.Milestone
		for _, milestone := range milestones {
			if milestone.Status != generated.MilestoneStatusCancelled {
				active = append(active, milestone)
			}
		}
		if len(active) == 1 {
			return active
		}
		return active
	}
	if id, err := uuid.Parse(query); err == nil {
		for _, milestone := range milestones {
			if milestone.ID == id {
				return []generated.Milestone{milestone}
			}
		}
		return nil
	}
	normalized := strings.ToLower(query)
	var matches []generated.Milestone
	for _, milestone := range milestones {
		if strings.Contains(strings.ToLower(milestone.Name), normalized) {
			matches = append(matches, milestone)
		}
	}
	slices.SortFunc(matches, func(a, b generated.Milestone) int {
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
	if len(matches) > 1 && strings.EqualFold(matches[0].Name, query) {
		return matches[:1]
	}
	return matches
}

func attentionTasks(
	tasks []generated.Task,
	now time.Time,
	timezone *time.Location,
) []TaskSummary {
	filtered := make([]generated.Task, 0)
	for _, task := range tasks {
		if task.ArchivedAt.IsSet() {
			continue
		}
		if task.Status == generated.TaskStatusDone || task.Status == generated.TaskStatusCancelled {
			continue
		}
		if task.Blocked || taskIsOverdue(task, now, timezone) {
			filtered = append(filtered, task)
		}
	}
	slices.SortFunc(filtered, func(a, b generated.Task) int {
		aOverdue := taskIsOverdue(a, now, timezone)
		bOverdue := taskIsOverdue(b, now, timezone)
		if aOverdue != bOverdue {
			if aOverdue {
				return -1
			}
			return 1
		}
		if a.Blocked != b.Blocked {
			if a.Blocked {
				return -1
			}
			return 1
		}
		aDue, aHasDue := a.DueDate.Get()
		bDue, bHasDue := b.DueDate.Get()
		if aHasDue != bHasDue {
			if aHasDue {
				return -1
			}
			return 1
		}
		if aHasDue && !aDue.Equal(bDue) {
			if aDue.Before(bDue) {
				return -1
			}
			return 1
		}
		switch {
		case a.Number < b.Number:
			return -1
		case a.Number > b.Number:
			return 1
		default:
			return 0
		}
	})
	if len(filtered) > maxAttentionTasks {
		filtered = filtered[:maxAttentionTasks]
	}
	result := make([]TaskSummary, 0, len(filtered))
	for _, task := range filtered {
		result = append(result, taskSummary(task, now, timezone))
	}
	return result
}

func taskSummary(
	task generated.Task,
	now time.Time,
	timezone *time.Location,
) TaskSummary {
	result := TaskSummary{
		Number: task.Number, Title: task.Title, Status: string(task.Status),
		Priority: string(task.Priority), ProjectNumber: task.Project.Number,
		ProjectName: task.Project.Name, Blocked: task.Blocked,
		Overdue: taskIsOverdue(task, now, timezone),
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

func taskIsOverdue(task generated.Task, now time.Time, timezone *time.Location) bool {
	if task.Status == generated.TaskStatusDone || task.Status == generated.TaskStatusCancelled {
		return false
	}
	dueDate, ok := task.DueDate.Get()
	if !ok {
		return false
	}
	if timezone == nil {
		timezone = time.UTC
	}
	today := now.In(timezone)
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, timezone)
	return dueDate.Before(todayDate)
}

func addTaskStatus(counts *TaskStatusCounts, status generated.TaskStatus) {
	switch status {
	case generated.TaskStatusTodo:
		counts.Todo++
	case generated.TaskStatusInProgress:
		counts.InProgress++
	case generated.TaskStatusInReview:
		counts.InReview++
	case generated.TaskStatusDone:
		counts.Done++
	case generated.TaskStatusCancelled:
		counts.Cancelled++
	}
}

func taskStatus(value string) (generated.TaskStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "todo":
		return generated.TaskStatusTodo, nil
	case "in_progress":
		return generated.TaskStatusInProgress, nil
	case "in_review":
		return generated.TaskStatusInReview, nil
	case "done":
		return generated.TaskStatusDone, nil
	case "cancelled":
		return generated.TaskStatusCancelled, nil
	default:
		return "", fmt.Errorf("%w: invalid Task status", ErrToolInput)
	}
}
