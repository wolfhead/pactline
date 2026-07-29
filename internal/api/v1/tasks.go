package v1

import (
	"context"
	"fmt"

	baseapi "bountyboard/internal/api"
	generated "bountyboard/internal/api/v1generated"
	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/identity"
	"bountyboard/internal/store"

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
	task := domain.Task{
		Title: req.Title, Description: req.Description.Or(""),
		CreatorID: subjectID,
	}
	if value, ok := req.Status.Get(); ok {
		task.Status = domain.TaskStatus(value)
	}
	if value, ok := req.Priority.Get(); ok {
		task.Priority = domain.TaskPriority(value)
	}
	if value, ok := req.AssigneeID.Get(); ok {
		task.AssigneeID = &value
	}
	if value, ok := req.DueDate.Get(); ok {
		task.DueDate = &value
	}
	var projectNumber *int64
	if value, ok := req.ProjectNumber.Get(); ok {
		projectNumber = &value
	}
	var milestoneID *uuid.UUID
	if value, ok := req.MilestoneID.Get(); ok {
		milestoneID = &value
	}
	created, err := h.Tasks.Create(
		ctx, task, req.LabelIds, projectNumber, milestoneID, actor,
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
	task, err := h.Tasks.Tasks.GetByNumber(ctx, params.Number)
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
	if value, ok := params.Cursor.Get(); ok {
		filter.Cursor = value
	}
	if value, ok := params.Limit.Get(); ok {
		filter.Limit = value
	}
	if value, ok := params.Q.Get(); ok {
		filter.Search = value
	}
	for _, value := range params.Status {
		filter.Statuses = append(filter.Statuses, domain.TaskStatus(value))
	}
	for _, value := range params.Priority {
		filter.Priorities = append(filter.Priorities, domain.TaskPriority(value))
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
		project, err := h.Tasks.Projects.Projects.GetByNumber(ctx, value)
		if err != nil {
			return nil, err
		}
		filter.ProjectID = &project.Project.ID
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
	var patch domain.TaskPatch
	if value, ok := req.Title.Get(); ok {
		patch.Title = &value
	}
	if value, ok := req.Description.Get(); ok {
		patch.Description = &value
	}
	if value, ok := req.Status.Get(); ok {
		status := domain.TaskStatus(value)
		patch.Status = &status
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
	if req.LabelIds != nil {
		patch.LabelsSet = true
		patch.LabelIDs = req.LabelIds
	}
	association := application.TaskAssociationPatch{
		ProjectNumberSet: req.ProjectNumber.IsSet(),
		MilestoneSet:     req.MilestoneID.IsSet(),
	}
	if value, ok := req.ProjectNumber.Get(); ok {
		association.ProjectNumber = &value
	}
	if value, ok := req.MilestoneID.Get(); ok {
		association.MilestoneID = &value
	}
	updated, err := h.Tasks.Update(
		ctx, params.Number, expectedVersion, patch, association, actor,
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
	updated, err := h.Tasks.SetArchived(
		ctx, number, expectedVersion, archived, actor,
	)
	if err != nil {
		return nil, err
	}
	return taskResponse(ctx, updated), nil
}

func (h *Handler) ListTaskComments(
	ctx context.Context,
	params generated.ListTaskCommentsParams,
) (generated.ListTaskCommentsRes, error) {
	task, err := h.Tasks.Tasks.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	comments, err := h.Tasks.Comments.List(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	offset, end, next, err := pageBounds(len(comments), params.Cursor, params.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]generated.Comment, end-offset)
	for i, comment := range comments[offset:end] {
		items[i] = commentFromDomain(comment)
	}
	response := generated.CommentList{Items: items}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.CommentListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) CreateTaskComment(
	ctx context.Context,
	req *generated.CommentWrite,
	params generated.CreateTaskCommentParams,
) (generated.CreateTaskCommentRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, subjectID, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	created, err := h.Tasks.CreateComment(
		ctx, params.Number, expectedVersion, subjectID, req.Body, actor,
	)
	if err != nil {
		return nil, err
	}
	comment := commentFromDomain(created.Comment)
	return &generated.CommentCreatedHeaders{
		Etag: generated.NewOptString(formatETag(comment.Version)),
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/tasks/%d/comments/%s", params.Number, comment.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   comment,
	}, nil
}

func (h *Handler) UpdateTaskComment(
	ctx context.Context,
	req *generated.CommentWrite,
	params generated.UpdateTaskCommentParams,
) (generated.UpdateTaskCommentRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := h.Tasks.UpdateComment(
		ctx, params.Number, params.ID, expectedVersion, req.Body, actor,
	)
	if err != nil {
		return nil, err
	}
	comment := commentFromDomain(updated)
	return &generated.CommentHeaders{
		Etag:       generated.NewOptString(formatETag(comment.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   comment,
	}, nil
}

func (h *Handler) DeleteTaskComment(
	ctx context.Context,
	params generated.DeleteTaskCommentParams,
) (generated.DeleteTaskCommentRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Tasks.DeleteComment(
		ctx, params.Number, params.ID, expectedVersion, actor,
	); err != nil {
		return nil, err
	}
	return &generated.NoContent{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
	}, nil
}

func (h *Handler) ListTaskActivity(
	ctx context.Context,
	params generated.ListTaskActivityParams,
) (generated.ListTaskActivityRes, error) {
	task, err := h.Tasks.Tasks.GetByNumber(ctx, params.Number)
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
		Title: task.Task.Title, Description: task.Task.Description,
		Status:    generated.TaskStatus(task.Task.Status),
		Priority:  generated.TaskPriority(task.Task.Priority),
		Creator:   userRefFromDomain(task.Creator),
		Labels:    make([]generated.Label, len(task.Labels)),
		CreatedAt: task.Task.CreatedAt, UpdatedAt: task.Task.UpdatedAt,
	}
	for i, label := range task.Labels {
		out.Labels[i] = labelFromDomain(label)
	}
	if task.Assignee != nil {
		out.Assignee = generated.NewOptUserRef(userRefFromDomain(*task.Assignee))
	}
	if task.Task.DueDate != nil {
		out.DueDate = generated.NewOptDate(*task.Task.DueDate)
	}
	if task.Project != nil {
		out.Project = generated.NewOptProjectRef(generated.ProjectRef{
			ID: task.Project.ID, Number: task.Project.Number, Name: task.Project.Name,
		})
	}
	if task.Milestone != nil {
		out.Milestone = generated.NewOptMilestoneRef(generated.MilestoneRef{
			ID: task.Milestone.ID, Name: task.Milestone.Name,
		})
	}
	if task.Task.CompletedAt != nil {
		out.CompletedAt = generated.NewOptDateTime(*task.Task.CompletedAt)
	}
	if task.Task.ArchivedAt != nil {
		out.ArchivedAt = generated.NewOptDateTime(*task.Task.ArchivedAt)
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

func commentFromDomain(comment domain.Comment) generated.Comment {
	return generated.Comment{
		ID: comment.ID, TaskID: comment.TaskID, AuthorID: comment.AuthorID,
		Body: comment.Body, Version: comment.Version,
		CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
	}
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
