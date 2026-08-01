package tools

import (
	"context"
	"testing"
	"time"

	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type readClientStub struct {
	tasks          []generated.Task
	project        generated.ProjectDetail
	listTaskParams generated.ListTasksParams
}

func (s *readClientStub) ListTasks(
	_ context.Context,
	params generated.ListTasksParams,
) (generated.ListTasksRes, error) {
	s.listTaskParams = params
	return &generated.TaskListHeaders{
		Response: generated.TaskList{Items: s.tasks},
	}, nil
}

func (s *readClientStub) GetTask(
	_ context.Context,
	params generated.GetTaskParams,
) (generated.GetTaskRes, error) {
	for _, task := range s.tasks {
		if task.Number == params.Number {
			return &generated.TaskHeaders{Response: task}, nil
		}
	}
	return &generated.ProblemStatusCodeWithHeaders{StatusCode: 404}, nil
}

func (s *readClientStub) GetProject(
	context.Context,
	generated.GetProjectParams,
) (generated.GetProjectRes, error) {
	return &generated.ProjectDetailHeaders{Response: s.project}, nil
}

func (*readClientStub) ListProjects(
	context.Context,
	generated.ListProjectsParams,
) (generated.ListProjectsRes, error) {
	panic("unexpected ListProjects call")
}

func (*readClientStub) ListUsers(
	context.Context,
	generated.ListUsersParams,
) (generated.ListUsersRes, error) {
	panic("unexpected ListUsers call")
}

func (*readClientStub) CreateTask(
	context.Context,
	*generated.TaskCreate,
	generated.CreateTaskParams,
) (generated.CreateTaskRes, error) {
	panic("unexpected CreateTask call")
}

func TestSearchTasksUsesBoundedOpenAPIFiltersAndTenantDate(t *testing.T) {
	timezone := time.FixedZone("UTC+8", 8*60*60)
	due := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	projectNumber := int64(7)
	client := &readClientStub{tasks: []generated.Task{
		taskFixture(1, "Overdue work", generated.TaskStatusInProgress, &due, nil, false),
		taskFixture(2, "Current work", generated.TaskStatusTodo, nil, nil, false),
		taskFixture(3, "Extra work", generated.TaskStatusDone, nil, nil, false),
	}}

	result, err := searchTasks(
		context.Background(),
		client,
		time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC),
		timezone,
		TaskSearchInput{
			Query: "work", ProjectNumber: &projectNumber,
			Statuses: []string{"todo", "in_progress"}, Limit: 2,
		},
	)

	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.True(t, result.Truncated)
	require.True(t, result.Items[0].Overdue, "UTC+8 date is already 2026-07-31")
	require.Equal(t, 3, client.listTaskParams.Limit.Value)
	require.Equal(t, int64(7), client.listTaskParams.ProjectNumber.Value)
	require.Equal(t, []generated.TaskStatus{
		generated.TaskStatusTodo, generated.TaskStatusInProgress,
	}, client.listTaskParams.Status)
}

func TestProjectOverviewComputesStatusBacklogAndAttentionDeterministically(t *testing.T) {
	timezone := time.UTC
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	overdue := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	milestoneID := uuid.New()
	client := &readClientStub{project: generated.ProjectDetail{
		Project: generated.Project{
			Number: 7, Name: "Launch", Creator: generated.UserRef{Name: "Ada"},
		},
		Milestones: []generated.Milestone{{
			ID: milestoneID, Name: "Beta", Status: generated.MilestoneStatusActive,
		}},
		Tasks: []generated.Task{
			taskFixture(1, "Backlog", generated.TaskStatusTodo, nil, nil, false),
			taskFixture(2, "Late", generated.TaskStatusInProgress, &overdue, &milestoneID, false),
			taskFixture(3, "Blocked", generated.TaskStatusInReview, nil, &milestoneID, true),
			taskFixture(4, "Done", generated.TaskStatusDone, &overdue, &milestoneID, false),
		},
	}}

	result, err := getProjectOverview(
		context.Background(), client, now, timezone,
		ProjectOverviewInput{ProjectNumber: 7},
	)

	require.NoError(t, err)
	require.Equal(t, 4, result.TaskCount)
	require.Equal(t, 1, result.BacklogCount)
	require.Equal(t, 1, result.OverdueCount)
	require.Equal(t, 1, result.BlockedCount)
	require.Equal(t, TaskStatusCounts{
		Todo: 1, InProgress: 1, InReview: 1, Done: 1,
	}, result.StatusCounts)
	require.Len(t, result.Milestones, 1)
	require.InDelta(t, 1.0/3.0, result.Milestones[0].CompletionRatio, 0.001)
	require.Equal(t, []int64{2, 3}, []int64{
		result.AttentionTasks[0].Number,
		result.AttentionTasks[1].Number,
	})
}

func TestMilestoneOverviewReturnsCandidatesInsteadOfGuessing(t *testing.T) {
	client := &readClientStub{project: generated.ProjectDetail{
		Project: generated.Project{Number: 7, Name: "Launch"},
		Milestones: []generated.Milestone{
			{ID: uuid.New(), Name: "Beta Web", Status: generated.MilestoneStatusActive},
			{ID: uuid.New(), Name: "Beta API", Status: generated.MilestoneStatusPlanned},
		},
	}}

	result, err := getMilestoneOverview(
		context.Background(), client, time.Now(), time.UTC,
		MilestoneOverviewInput{ProjectNumber: 7, Query: "Beta"},
	)

	require.NoError(t, err)
	require.Nil(t, result.Overview)
	require.Len(t, result.Candidates, 2)
}

func taskFixture(
	number int64,
	title string,
	status generated.TaskStatus,
	dueDate *time.Time,
	milestoneID *uuid.UUID,
	blocked bool,
) generated.Task {
	task := generated.Task{
		Number: number, Title: title, Status: status,
		Priority: generated.TaskPriorityMedium,
		Project:  generated.ProjectRef{Number: 7, Name: "Launch"},
		Creator:  generated.UserRef{ID: uuid.New(), Name: "Ada"},
		Blocked:  blocked,
	}
	if dueDate != nil {
		task.DueDate = generated.NewOptDate(*dueDate)
	}
	if milestoneID != nil {
		task.Milestone = generated.NewOptMilestoneRef(generated.MilestoneRef{
			ID: *milestoneID, Name: "Beta",
		})
	}
	return task
}
