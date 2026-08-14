package v1

import (
	"context"
	"fmt"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

func (h *Handler) CreateTask(
	ctx context.Context,
	req *generated.TaskCreate,
	_ generated.CreateTaskParams,
) (generated.CreateTaskRes, error) {
	actor, subjectID, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.Access.RequireProjectByNumber(
		ctx, req.ProjectNumber, subject, application.ProjectPermissionWrite,
	); err != nil {
		return nil, err
	}
	task := domain.Task{
		Title: req.Title, Context: req.Context,
		ExpectedResult: req.ExpectedResult,
		Description:    req.Description.Or(""),
		CreatorID:      subjectID,
	}
	if value, ok := req.Priority.Get(); ok {
		task.Priority = domain.TaskPriority(value)
	}
	if value, ok := req.AssigneeID.Get(); ok {
		task.AssigneeID = &value
	}
	if value, ok := req.StartDate.Get(); ok {
		task.StartDate = &value
	}
	if value, ok := req.DueDate.Get(); ok {
		task.DueDate = &value
	}
	projectNumber := req.ProjectNumber
	var milestoneID *uuid.UUID
	if value, ok := req.MilestoneID.Get(); ok {
		milestoneID = &value
	}
	var parentNumber *int64
	if value, ok := req.ParentNumber.Get(); ok {
		parentNumber = &value
	}
	created, err := h.Tasks.Create(
		ctx, task, req.LabelIds, &projectNumber, milestoneID,
		parentNumber, req.DependencyNumbers, actor,
	)
	if err != nil {
		return nil, err
	}
	response := taskFromDomain(created)
	return &generated.TaskCreatedHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		Location:   generated.NewOptString(fmt.Sprintf("/api/v1/tasks/%d", response.Number)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) GetTask(
	ctx context.Context,
	params generated.GetTaskParams,
) (generated.GetTaskRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(
		ctx, params.Number, subject, application.ProjectPermissionRead,
	)
	if err != nil {
		return nil, err
	}
	response := taskFromDomain(task)
	return &generated.TaskHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) ListTasks(
	ctx context.Context,
	params generated.ListTasksParams,
) (generated.ListTasksRes, error) {
	filter := store.TaskListFilter{
		LabelIDs: params.Label,
		Sort:     "updated_at",
		Order:    "desc",
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	if !subject.IsPlatformAdministrator() {
		filter.VisibleToUserID = &subject.UserID
	}
	if value, ok := params.Cursor.Get(); ok {
		filter.Cursor = value
	}
	if value, ok := params.Limit.Get(); ok {
		filter.Limit = value
	}
	if value, ok := params.Q.Get(); ok {
		filter.Search = value
	}
	for _, value := range params.Phase {
		filter.Phases = append(filter.Phases, domain.TaskPhase(value))
	}
	for _, value := range params.Priority {
		filter.Priorities = append(filter.Priorities, domain.TaskPriority(value))
	}
	for _, value := range params.Activity {
		filter.Activities = append(filter.Activities, domain.TaskActivityState(value))
	}
	if value, ok := params.ClaimableStage.Get(); ok {
		filter.ClaimableStage = domain.TaskClaimStage(value)
	}
	if value, ok := params.Assignee.Get(); ok {
		if value == "none" {
			filter.Unassigned = true
		} else {
			assigneeID, err := uuid.Parse(value)
			if err != nil {
				return nil, ErrInvalidRequest
			}
			filter.AssigneeID = &assigneeID
		}
	}
	if value, ok := params.ProjectNumber.Get(); ok {
		project, err := h.Access.RequireProjectByNumber(
			ctx, value, subject, application.ProjectPermissionRead,
		)
		if err != nil {
			return nil, err
		}
		filter.ProjectID = &project.Project.ID
	}
	if value, ok := params.MilestoneID.Get(); ok {
		filter.MilestoneID = &value
	}
	if value, ok := params.CreatorID.Get(); ok {
		filter.CreatorID = &value
	}
	if value, ok := params.BacklogOnly.Get(); ok {
		filter.BacklogOnly = value
	}
	if value, ok := params.Archived.Get(); ok {
		filter.Archived = string(value)
	}
	if value, ok := params.Sort.Get(); ok {
		filter.Sort = string(value)
	}
	if value, ok := params.Order.Get(); ok {
		filter.Order = string(value)
	}
	result, err := h.Tasks.Tasks.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]generated.Task, len(result.Items))
	for i, task := range result.Items {
		items[i] = taskFromDomain(task)
	}
	response := generated.TaskList{Items: items}
	if result.NextCursor != "" {
		response.NextCursor = generated.NewOptString(result.NextCursor)
	}
	return &generated.TaskListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) UpdateTask(
	ctx context.Context,
	req *generated.TaskPatch,
	params generated.UpdateTaskParams,
) (generated.UpdateTaskRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.Access.RequireTaskByNumber(
		ctx, params.Number, subject, application.ProjectPermissionWrite,
	); err != nil {
		return nil, err
	}
	var patch domain.TaskPatch
	if value, ok := req.Title.Get(); ok {
		patch.Title = &value
	}
	if value, ok := req.Context.Get(); ok {
		patch.Context = &value
	}
	if value, ok := req.ExpectedResult.Get(); ok {
		patch.ExpectedResult = &value
	}
	if value, ok := req.Description.Get(); ok {
		patch.Description = &value
	}
	if value, ok := req.Priority.Get(); ok {
		priority := domain.TaskPriority(value)
		patch.Priority = &priority
	}
	if req.AssigneeID.IsSet() {
		patch.AssigneeSet = true
		if value, ok := req.AssigneeID.Get(); ok {
			patch.AssigneeID = &value
		}
	}
	if req.DueDate.IsSet() {
		patch.DueDateSet = true
		if value, ok := req.DueDate.Get(); ok {
			patch.DueDate = &value
		}
	}
	if req.StartDate.IsSet() {
		patch.StartDateSet = true
		if value, ok := req.StartDate.Get(); ok {
			patch.StartDate = &value
		}
	}
	if req.LabelIds != nil {
		patch.LabelsSet = true
		patch.LabelIDs = req.LabelIds
	}
	association := application.TaskAssociationPatch{
		MilestoneSet: req.MilestoneID.IsSet(),
	}
	if value, ok := req.MilestoneID.Get(); ok {
		association.MilestoneID = &value
	}
	relationships := application.TaskRelationshipPatch{
		ParentSet:       req.ParentNumber.IsSet(),
		DependenciesSet: req.DependencyNumbers != nil,
	}
	if value, ok := req.ParentNumber.Get(); ok {
		relationships.ParentNumber = &value
	}
	if req.DependencyNumbers != nil {
		relationships.DependencyNumbers = req.DependencyNumbers
	}
	if value, ok := req.ScheduleShiftDays.Get(); ok {
		patch.ScheduleShiftDays = &value
	}
	updated, err := h.Tasks.Update(
		ctx, params.Number, expectedVersion, patch, association, relationships, actor,
	)
	if err != nil {
		return nil, err
	}
	return taskResponse(ctx, updated), nil
}

func (h *Handler) ArchiveTask(
	ctx context.Context,
	params generated.ArchiveTaskParams,
) (generated.ArchiveTaskRes, error) {
	return h.setTaskArchived(ctx, params.Number, params.IfMatch, true)
}

func (h *Handler) RestoreTask(
	ctx context.Context,
	params generated.RestoreTaskParams,
) (generated.RestoreTaskRes, error) {
	return h.setTaskArchived(ctx, params.Number, params.IfMatch, false)
}

func (h *Handler) setTaskArchived(
	ctx context.Context,
	number int64,
	ifMatch string,
	archived bool,
) (*generated.TaskHeaders, error) {
	expectedVersion, err := parseIfMatch(ifMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.Access.RequireTaskByNumber(
		ctx, number, subject, application.ProjectPermissionWrite,
	); err != nil {
		return nil, err
	}
	updated, err := h.Tasks.SetArchived(
		ctx, number, expectedVersion, archived, actor,
	)
	if err != nil {
		return nil, err
	}
	return taskResponse(ctx, updated), nil
}

func (h *Handler) ListTaskActivity(
	ctx context.Context,
	params generated.ListTaskActivityParams,
) (generated.ListTaskActivityRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(
		ctx, params.Number, subject, application.ProjectPermissionRead,
	)
	if err != nil {
		return nil, err
	}
	activity, err := h.Tasks.Tasks.ListActivity(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	offset, end, next, err := pageBounds(len(activity), params.Cursor, params.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]generated.TaskActivity, end-offset)
	for i, entry := range activity[offset:end] {
		items[i] = activityFromDomain(entry)
	}
	response := generated.TaskActivityList{Items: items}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.TaskActivityListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) ListLabels(
	ctx context.Context,
	params generated.ListLabelsParams,
) (generated.ListLabelsRes, error) {
	labels, err := h.Labels.Labels.List(ctx)
	if err != nil {
		return nil, err
	}
	offset, end, next, err := pageBounds(len(labels), params.Cursor, params.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]generated.Label, end-offset)
	for i, label := range labels[offset:end] {
		items[i] = labelFromDomain(label)
	}
	response := generated.LabelList{Items: items}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.LabelListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) CreateLabel(
	ctx context.Context,
	req *generated.LabelWrite,
	_ generated.CreateLabelParams,
) (generated.CreateLabelRes, error) {
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	created, err := h.Labels.Create(ctx, req.Name, actor)
	if err != nil {
		return nil, err
	}
	label := labelFromDomain(created)
	return &generated.LabelCreatedHeaders{
		Etag:       generated.NewOptString(formatETag(label.Version)),
		Location:   generated.NewOptString("/api/v1/labels/" + label.ID.String()),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   label,
	}, nil
}

func (h *Handler) UpdateLabel(
	ctx context.Context,
	req *generated.LabelWrite,
	params generated.UpdateLabelParams,
) (generated.UpdateLabelRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := h.Labels.Rename(
		ctx, params.ID, expectedVersion, req.Name, actor,
	)
	if err != nil {
		return nil, err
	}
	label := labelFromDomain(updated)
	return &generated.LabelHeaders{
		Etag:       generated.NewOptString(formatETag(label.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   label,
	}, nil
}

func (h *Handler) DeleteLabel(
	ctx context.Context,
	params generated.DeleteLabelParams,
) (generated.DeleteLabelRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Labels.Delete(ctx, params.ID, expectedVersion, actor); err != nil {
		return nil, err
	}
	return &generated.NoContent{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
	}, nil
}

func operationContext(ctx context.Context) (domain.OperationActor, uuid.UUID, error) {
	actor, ok := baseapi.OperationActorFromContext(ctx)
	if !ok {
		return domain.OperationActor{}, uuid.Nil, ErrAuthenticationRequired
	}
	current, ok := identity.FromContext(ctx)
	if !ok {
		return domain.OperationActor{}, uuid.Nil, ErrAuthenticationRequired
	}
	return actor, current.Subject.ID, nil
}

func taskResponse(ctx context.Context, task store.TaskWithRelations) *generated.TaskHeaders {
	response := taskFromDomain(task)
	return &generated.TaskHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}
}

func taskFromDomain(task store.TaskWithRelations) generated.Task {
	out := generated.Task{
		ID: task.Task.ID, Number: task.Task.Number, Version: task.Task.Version,
		Title: task.Task.Title, Context: task.Task.Context,
		ExpectedResult: task.Task.ExpectedResult, Description: task.Task.Description,
		Phase:        generated.TaskPhase(task.Task.Phase),
		ReviewCycle:  task.Task.ReviewCycle,
		MainThreadID: task.Task.MainThreadID,
		Priority:     generated.TaskPriority(task.Task.Priority),
		Creator:      userRefFromDomain(task.Creator),
		Labels:       make([]generated.Label, len(task.Labels)),
		Children:     make([]generated.TaskRelationRef, len(task.Children)),
		Dependencies: make([]generated.TaskRelationRef, len(task.Dependencies)),
		Dependents:   make([]generated.TaskRelationRef, len(task.Dependents)),
		Blocked:      task.Blocked,
		CreatedAt:    task.Task.CreatedAt, UpdatedAt: task.Task.UpdatedAt,
	}
	if task.Task.Activity != "" {
		out.Activity = generated.NewOptTaskActivityState(
			generated.TaskActivityState(task.Task.Activity),
		)
	}
	if task.Task.ActiveIssueThreadID != nil {
		out.ActiveIssueThreadID = generated.NewOptUUID(*task.Task.ActiveIssueThreadID)
	}
	for i, label := range task.Labels {
		out.Labels[i] = labelFromDomain(label)
	}
	if task.Assignee != nil {
		out.Assignee = generated.NewOptUserRef(userRefFromDomain(*task.Assignee))
	}
	if task.Task.StartDate != nil {
		out.StartDate = generated.NewOptDate(*task.Task.StartDate)
	}
	if task.Task.DueDate != nil {
		out.DueDate = generated.NewOptDate(*task.Task.DueDate)
	}
	out.Project = generated.ProjectRef{
		ID: task.Project.ID, Number: task.Project.Number, Name: task.Project.Name,
	}
	if task.Milestone != nil {
		out.Milestone = generated.NewOptMilestoneRef(generated.MilestoneRef{
			ID: task.Milestone.ID, Name: task.Milestone.Name,
		})
	}
	if task.Parent != nil {
		out.Parent = generated.NewOptTaskRelationRef(taskRelationRefFromDomain(*task.Parent))
	}
	for i, child := range task.Children {
		out.Children[i] = taskRelationRefFromDomain(child)
	}
	for i, dependency := range task.Dependencies {
		out.Dependencies[i] = taskRelationRefFromDomain(dependency)
	}
	for i, dependent := range task.Dependents {
		out.Dependents[i] = taskRelationRefFromDomain(dependent)
	}
	if task.Task.CompletedAt != nil {
		out.CompletedAt = generated.NewOptDateTime(*task.Task.CompletedAt)
	}
	if task.Task.ArchivedAt != nil {
		out.ArchivedAt = generated.NewOptDateTime(*task.Task.ArchivedAt)
	}
	return out
}

func taskRelationRefFromDomain(task store.TaskRelationRef) generated.TaskRelationRef {
	out := generated.TaskRelationRef{
		ID:       task.ID,
		Number:   task.Number,
		Title:    task.Title,
		Phase:    generated.TaskPhase(task.Phase),
		Archived: task.Archived,
	}
	if task.Milestone != nil {
		out.Milestone = generated.NewOptMilestoneRef(generated.MilestoneRef{
			ID:   task.Milestone.ID,
			Name: task.Milestone.Name,
		})
	}
	return out
}

func userRefFromDomain(user domain.UserRef) generated.UserRef {
	out := generated.UserRef{ID: user.ID, Name: user.Name}
	if user.Email != nil {
		out.Email = generated.NewOptString(*user.Email)
	}
	return out
}

func labelFromDomain(label domain.Label) generated.Label {
	return generated.Label{
		ID: label.ID, Name: label.Name, Version: label.Version, CreatedAt: label.CreatedAt,
	}
}

func activityFromDomain(activity domain.Activity) generated.TaskActivity {
	out := generated.TaskActivity{
		ID: activity.ID, ActorID: activity.ActorID, Field: string(activity.Field),
		CreatedAt: activity.CreatedAt,
	}
	if activity.OldValue != nil {
		out.OldValue = generated.NewOptString(*activity.OldValue)
	}
	if activity.NewValue != nil {
		out.NewValue = generated.NewOptString(*activity.NewValue)
	}
	if activity.AuthMethod != nil {
		out.AuthenticationMethod = generated.NewOptTaskActivityAuthenticationMethod(
			generated.TaskActivityAuthenticationMethod(*activity.AuthMethod),
		)
	}
	if activity.TokenName != nil {
		out.TokenName = generated.NewOptString(*activity.TokenName)
	}
	if activity.RequestID != nil {
		out.RequestID = generated.NewOptString(*activity.RequestID)
	}
	return out
}

func pageBounds(
	total int,
	cursor generated.OptString,
	limitOption generated.OptInt,
) (offset, end int, next string, err error) {
	offset, err = decodeCursor(cursor)
	if err != nil || offset > total {
		return 0, 0, "", ErrInvalidRequest
	}
	limit := 50
	if value, ok := limitOption.Get(); ok {
		limit = value
	}
	end = min(total, offset+limit)
	if end < total {
		next = encodeCursor(end)
	}
	return offset, end, next, nil
}
