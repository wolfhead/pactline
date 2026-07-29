package v1

import (
	"context"
	"fmt"

	baseapi "bountyboard/internal/api"
	generated "bountyboard/internal/api/v1generated"
	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

func (h *Handler) CreateProject(
	ctx context.Context,
	req *generated.ProjectCreate,
	_ generated.CreateProjectParams,
) (generated.CreateProjectRes, error) {
	if err := requireAdministrator(ctx); err != nil {
		return nil, err
	}
	actor, subjectID, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	project := domain.Project{
		Name: req.Name, Description: req.Description.Or(""),
		OwnerID: req.OwnerID, CreatorID: subjectID,
	}
	created, err := h.Projects.Projects.CreateWithOperation(ctx, project, actor)
	if err != nil {
		return nil, err
	}
	response := projectFromDomain(created)
	return &generated.ProjectCreatedHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		Location:   generated.NewOptString(fmt.Sprintf("/api/v1/projects/%d", response.Number)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) GetProject(
	ctx context.Context,
	params generated.GetProjectParams,
) (generated.GetProjectRes, error) {
	detail, err := h.Projects.GetDetail(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	response := projectDetailFromDomain(detail)
	return &generated.ProjectDetailHeaders{
		Etag:       generated.NewOptString(formatETag(response.Project.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) ListProjects(
	ctx context.Context,
	params generated.ListProjectsParams,
) (generated.ListProjectsRes, error) {
	includeArchived := false
	if value, ok := params.Archived.Get(); ok {
		includeArchived = value == generated.ListProjectsArchivedAll
	}
	projects, err := h.Projects.Projects.List(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	offset, end, next, err := pageBounds(len(projects), params.Cursor, params.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]generated.Project, end-offset)
	for i, project := range projects[offset:end] {
		items[i] = projectFromDomain(project)
	}
	response := generated.ProjectList{Items: items}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.ProjectListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) UpdateProject(
	ctx context.Context,
	req *generated.ProjectPatch,
	params generated.UpdateProjectParams,
) (generated.UpdateProjectRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	var patch store.ProjectPatch
	if value, ok := req.Name.Get(); ok {
		patch.Name = &value
	}
	if value, ok := req.Description.Get(); ok {
		patch.Description = &value
	}
	if value, ok := req.OwnerID.Get(); ok {
		patch.OwnerID = &value
	}
	updated, err := h.Projects.Projects.UpdateVersionedWithOperation(
		ctx, params.Number, expectedVersion, patch, actor,
	)
	if err != nil {
		return nil, err
	}
	return projectResponse(ctx, updated), nil
}

func (h *Handler) ArchiveProject(
	ctx context.Context,
	params generated.ArchiveProjectParams,
) (generated.ArchiveProjectRes, error) {
	return h.applyProjectLifecycle(
		ctx, params.Number, params.IfMatch, store.ProjectActionArchive, "",
	)
}

func (h *Handler) RestoreProject(
	ctx context.Context,
	params generated.RestoreProjectParams,
) (generated.RestoreProjectRes, error) {
	return h.applyProjectLifecycle(
		ctx, params.Number, params.IfMatch, store.ProjectActionRestore, "",
	)
}

func (h *Handler) applyProjectLifecycle(
	ctx context.Context,
	number int64,
	ifMatch string,
	action store.ProjectLifecycleAction,
	reason string,
) (*generated.ProjectHeaders, error) {
	if err := requireAdministrator(ctx); err != nil {
		return nil, err
	}
	expectedVersion, err := parseIfMatch(ifMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	userID := actor.UserID
	updated, err := h.Projects.Projects.ApplyLifecycleVersionedWithOperation(
		ctx, number, expectedVersion, action,
		domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		reason, actor,
	)
	if err != nil {
		return nil, err
	}
	return projectResponse(ctx, updated), nil
}

func (h *Handler) CreateMilestone(
	ctx context.Context,
	req *generated.MilestoneCreate,
	params generated.CreateMilestoneParams,
) (generated.CreateMilestoneRes, error) {
	expectedProjectVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	project, err := h.Projects.Projects.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	milestone := domain.Milestone{
		Name: req.Name, Outcome: req.Outcome, Description: req.Description.Or(""),
		OwnerID: req.OwnerID, Position: req.Position,
	}
	if value, ok := req.TargetDate.Get(); ok {
		milestone.TargetDate = &value
	}
	created, err := h.Projects.Milestones.CreateVersionedWithOperation(
		ctx, project.Project.ID, expectedProjectVersion, milestone, actor,
	)
	if err != nil {
		return nil, err
	}
	response := milestoneFromDomain(created.Milestone, nil)
	return &generated.MilestoneCreatedHeaders{
		Etag: generated.NewOptString(formatETag(response.Version)),
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/projects/%d/milestones/%s", params.Number, response.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) UpdateMilestone(
	ctx context.Context,
	req *generated.MilestonePatch,
	params generated.UpdateMilestoneParams,
) (generated.UpdateMilestoneRes, error) {
	expectedMilestoneVersion, expectedProjectVersion, err := milestonePreconditions(
		params.IfMatch, params.XProjectIfMatch,
	)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	project, err := h.Projects.Projects.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	var patch store.MilestonePatch
	if value, ok := req.Name.Get(); ok {
		patch.Name = &value
	}
	if value, ok := req.Outcome.Get(); ok {
		patch.Outcome = &value
	}
	if value, ok := req.Description.Get(); ok {
		patch.Description = &value
	}
	if value, ok := req.OwnerID.Get(); ok {
		patch.OwnerID = &value
	}
	if req.TargetDate.IsSet() {
		patch.TargetDateSet = true
		if value, ok := req.TargetDate.Get(); ok {
			patch.TargetDate = &value
		}
	}
	if value, ok := req.Position.Get(); ok {
		patch.Position = &value
	}
	updated, err := h.Projects.Milestones.UpdateVersionedWithOperation(
		ctx, project.Project.ID, expectedProjectVersion,
		params.ID, expectedMilestoneVersion, patch, actor,
	)
	if err != nil {
		return nil, err
	}
	return milestoneResponse(ctx, updated.Milestone), nil
}

func (h *Handler) CompleteMilestone(
	ctx context.Context,
	req generated.OptLifecycleRequest,
	params generated.CompleteMilestoneParams,
) (generated.CompleteMilestoneRes, error) {
	return h.applyMilestoneLifecycle(
		ctx, params.Number, params.ID, params.IfMatch, params.XProjectIfMatch,
		store.MilestoneActionComplete, optionalReason(req),
	)
}

func (h *Handler) ActivateMilestone(
	ctx context.Context,
	params generated.ActivateMilestoneParams,
) (generated.ActivateMilestoneRes, error) {
	return h.applyMilestoneLifecycle(
		ctx, params.Number, params.ID, params.IfMatch, params.XProjectIfMatch,
		store.MilestoneActionActivate, "",
	)
}

func (h *Handler) CancelMilestone(
	ctx context.Context,
	req generated.OptLifecycleRequest,
	params generated.CancelMilestoneParams,
) (generated.CancelMilestoneRes, error) {
	return h.applyMilestoneLifecycle(
		ctx, params.Number, params.ID, params.IfMatch, params.XProjectIfMatch,
		store.MilestoneActionCancel, optionalReason(req),
	)
}

func (h *Handler) ReopenMilestone(
	ctx context.Context,
	req *generated.LifecycleRequest,
	params generated.ReopenMilestoneParams,
) (generated.ReopenMilestoneRes, error) {
	return h.applyMilestoneLifecycle(
		ctx, params.Number, params.ID, params.IfMatch, params.XProjectIfMatch,
		store.MilestoneActionReopen, req.Reason.Or(""),
	)
}

func (h *Handler) applyMilestoneLifecycle(
	ctx context.Context,
	projectNumber int64,
	milestoneID uuid.UUID,
	milestoneIfMatch string,
	projectIfMatch string,
	action store.MilestoneLifecycleAction,
	reason string,
) (*generated.MilestoneHeaders, error) {
	expectedMilestoneVersion, expectedProjectVersion, err := milestonePreconditions(
		milestoneIfMatch, projectIfMatch,
	)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	project, err := h.Projects.Projects.GetByNumber(ctx, projectNumber)
	if err != nil {
		return nil, err
	}
	updated, err := h.Projects.Milestones.ApplyLifecycleVersionedWithOperation(
		ctx, project.Project.ID, expectedProjectVersion,
		milestoneID, expectedMilestoneVersion, action, actor, reason,
	)
	if err != nil {
		return nil, err
	}
	return milestoneResponse(ctx, updated.Milestone), nil
}

func (h *Handler) ListTaskCriteria(
	ctx context.Context,
	params generated.ListTaskCriteriaParams,
) (generated.ListTaskCriteriaRes, error) {
	task, err := h.Projects.Tasks.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	items, err := h.Projects.Acceptance.ListForTask(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	return criterionListResponse(ctx, items, params.Cursor, params.Limit)
}

func (h *Handler) CreateTaskCriterion(
	ctx context.Context,
	req *generated.CriterionCreate,
	params generated.CreateTaskCriterionParams,
) (generated.CreateTaskCriterionRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Projects.Tasks.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	created, err := h.Projects.Acceptance.CreateTaskCriterionVersioned(
		ctx, task.Task.ID, expectedVersion, criterionFromRequest(req), actor,
	)
	if err != nil {
		return nil, err
	}
	return criterionCreatedResponse(
		ctx, created.Criterion,
		fmt.Sprintf("/api/v1/tasks/%d/criteria/%s", params.Number, created.Criterion.ID),
	), nil
}

func (h *Handler) ListMilestoneCriteria(
	ctx context.Context,
	params generated.ListMilestoneCriteriaParams,
) (generated.ListMilestoneCriteriaRes, error) {
	project, err := h.Projects.Projects.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	if _, err := h.Projects.Milestones.Get(ctx, project.Project.ID, params.ID); err != nil {
		return nil, err
	}
	items, err := h.Projects.Acceptance.ListForMilestone(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return criterionListResponse(ctx, items, params.Cursor, params.Limit)
}

func (h *Handler) CreateMilestoneCriterion(
	ctx context.Context,
	req *generated.CriterionCreate,
	params generated.CreateMilestoneCriterionParams,
) (generated.CreateMilestoneCriterionRes, error) {
	expectedMilestoneVersion, expectedProjectVersion, err := milestonePreconditions(
		params.IfMatch, params.XProjectIfMatch,
	)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	project, err := h.Projects.Projects.GetByNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	created, err := h.Projects.Acceptance.CreateMilestoneCriterionVersioned(
		ctx, project.Project.ID, expectedProjectVersion,
		params.ID, expectedMilestoneVersion, criterionFromRequest(req), actor,
	)
	if err != nil {
		return nil, err
	}
	return criterionCreatedResponse(
		ctx, created.Criterion,
		fmt.Sprintf(
			"/api/v1/projects/%d/milestones/%s/criteria/%s",
			params.Number, params.ID, created.Criterion.ID,
		),
	), nil
}

func (h *Handler) UpdateCriterion(
	ctx context.Context,
	req *generated.CriterionPatch,
	params generated.UpdateCriterionParams,
) (generated.UpdateCriterionRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	var criterionText, instructions *string
	var position *int
	if value, ok := req.Criterion.Get(); ok {
		criterionText = &value
	}
	if value, ok := req.VerificationInstructions.Get(); ok {
		instructions = &value
	}
	if value, ok := req.Position.Get(); ok {
		position = &value
	}
	updated, err := h.Projects.Acceptance.UpdateCriterionVersioned(
		ctx, params.ID, expectedVersion, criterionText, instructions, position, actor,
	)
	if err != nil {
		return nil, err
	}
	response := acceptanceCriterionFromDomain(store.CriterionWithCurrentCheck{
		Criterion: updated.Criterion,
	})
	return &generated.CriterionHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) DeleteCriterion(
	ctx context.Context,
	req generated.OptLifecycleRequest,
	params generated.DeleteCriterionParams,
) (generated.DeleteCriterionRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Projects.Acceptance.RemoveCriterionVersioned(
		ctx, params.ID, expectedVersion, actor, optionalReason(req),
	); err != nil {
		return nil, err
	}
	return &generated.NoContent{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
	}, nil
}

func (h *Handler) CreateAcceptanceCheck(
	ctx context.Context,
	req *generated.AcceptanceCheckCreate,
	params generated.CreateAcceptanceCheckParams,
) (generated.CreateAcceptanceCheckRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, subjectID, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	checker := domain.Actor{Type: domain.ActorTypeUser, UserID: &subjectID}
	if actor.AuthMethod == domain.AuthenticationMethodAPIToken {
		checker = domain.Actor{Type: domain.ActorTypeAgent, Ref: actor.TokenName}
		if checker.Ref == "" {
			checker.Ref = "api-token"
		}
	}
	created, err := h.Projects.Acceptance.AddCheckVersioned(
		ctx, params.ID, expectedVersion, domain.AcceptanceCheck{
			CriterionID: params.ID, CriterionRevision: req.CriterionRevision,
			Outcome: domain.AcceptanceOutcome(req.Outcome), Evidence: req.Evidence,
			Checker: checker,
		}, actor,
	)
	if err != nil {
		return nil, err
	}
	response := acceptanceCheckFromDomain(created.Check)
	return &generated.AcceptanceCheckCreatedHeaders{
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/criteria/%s/checks/%s", params.ID, response.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func projectResponse(
	ctx context.Context,
	project store.ProjectWithRelations,
) *generated.ProjectHeaders {
	response := projectFromDomain(project)
	return &generated.ProjectHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}
}

func milestoneResponse(
	ctx context.Context,
	milestone domain.Milestone,
) *generated.MilestoneHeaders {
	response := milestoneFromDomain(milestone, nil)
	return &generated.MilestoneHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}
}

func projectFromDomain(project store.ProjectWithRelations) generated.Project {
	out := generated.Project{
		ID: project.Project.ID, Number: project.Project.Number,
		Version: project.Project.Version, Name: project.Project.Name,
		Description: project.Project.Description,
		Owner:       userRefFromDomain(project.Owner), Creator: userRefFromDomain(project.Creator),
		CreatedAt: project.Project.CreatedAt, UpdatedAt: project.Project.UpdatedAt,
		CompletedTasks: project.CompletedTasks, EligibleTasks: project.EligibleTasks,
	}
	if project.Project.ArchivedAt != nil {
		out.ArchivedAt = generated.NewOptDateTime(*project.Project.ArchivedAt)
	}
	return out
}

func projectDetailFromDomain(detail application.ProjectDetail) generated.ProjectDetail {
	out := generated.ProjectDetail{
		Project:    projectFromDomain(detail.Project),
		Milestones: make([]generated.Milestone, len(detail.Milestones)),
		Tasks:      make([]generated.Task, len(detail.Tasks)),
		Activity:   make([]generated.ProjectActivity, len(detail.Activity)),
	}
	for i, milestone := range detail.Milestones {
		out.Milestones[i] = milestoneFromDomain(
			milestone, detail.MilestoneCriteria[milestone.ID],
		)
	}
	for i, task := range detail.Tasks {
		out.Tasks[i] = taskFromDomain(task)
	}
	for i, activity := range detail.Activity {
		out.Activity[i] = projectActivityFromDomain(activity)
	}
	return out
}

func milestoneFromDomain(
	milestone domain.Milestone,
	criteria []store.CriterionWithCurrentCheck,
) generated.Milestone {
	out := generated.Milestone{
		ID: milestone.ID, ProjectID: milestone.ProjectID, Version: milestone.Version,
		Name: milestone.Name, Outcome: milestone.Outcome, Description: milestone.Description,
		OwnerID: milestone.OwnerID,
		Status:  generated.MilestoneStatus(milestone.Status), Position: milestone.Position,
		CreatedAt: milestone.CreatedAt, UpdatedAt: milestone.UpdatedAt,
		AcceptanceCriteria: make([]generated.AcceptanceCriterion, len(criteria)),
	}
	if milestone.TargetDate != nil {
		out.TargetDate = generated.NewOptDate(*milestone.TargetDate)
	}
	if milestone.CompletedAt != nil {
		out.CompletedAt = generated.NewOptDateTime(*milestone.CompletedAt)
	}
	if milestone.CancelledAt != nil {
		out.CancelledAt = generated.NewOptDateTime(*milestone.CancelledAt)
	}
	for i, criterion := range criteria {
		out.AcceptanceCriteria[i] = acceptanceCriterionFromDomain(criterion)
	}
	return out
}

func criterionFromRequest(req *generated.CriterionCreate) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{
		Criterion: req.Criterion, VerificationInstructions: req.VerificationInstructions,
		Position: req.Position,
	}
}

func criterionListResponse(
	ctx context.Context,
	criteria []store.CriterionWithCurrentCheck,
	cursor generated.OptString,
	limit generated.OptInt,
) (*generated.CriterionListHeaders, error) {
	offset, end, next, err := pageBounds(len(criteria), cursor, limit)
	if err != nil {
		return nil, err
	}
	items := make([]generated.AcceptanceCriterion, end-offset)
	for i, criterion := range criteria[offset:end] {
		items[i] = acceptanceCriterionFromDomain(criterion)
	}
	response := generated.CriterionList{Items: items}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.CriterionListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func criterionCreatedResponse(
	ctx context.Context,
	criterion domain.AcceptanceCriterion,
	location string,
) *generated.CriterionCreatedHeaders {
	response := acceptanceCriterionFromDomain(store.CriterionWithCurrentCheck{
		Criterion: criterion,
	})
	return &generated.CriterionCreatedHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		Location:   generated.NewOptString(location),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}
}

func acceptanceCriterionFromDomain(
	criterion store.CriterionWithCurrentCheck,
) generated.AcceptanceCriterion {
	value := criterion.Criterion
	out := generated.AcceptanceCriterion{
		ID: value.ID, Version: value.Version, Criterion: value.Criterion,
		VerificationInstructions: value.VerificationInstructions,
		Revision:                 value.Revision, Position: value.Position,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
	if value.MilestoneID != nil {
		out.MilestoneID = generated.NewOptUUID(*value.MilestoneID)
	}
	if value.TaskID != nil {
		out.TaskID = generated.NewOptUUID(*value.TaskID)
	}
	if criterion.CurrentCheck != nil {
		out.CurrentCheck = generated.NewOptAcceptanceCheck(
			acceptanceCheckFromDomain(*criterion.CurrentCheck),
		)
	}
	return out
}

func acceptanceCheckFromDomain(check domain.AcceptanceCheck) generated.AcceptanceCheck {
	out := generated.AcceptanceCheck{
		ID: check.ID, CriterionID: check.CriterionID,
		CriterionRevision: check.CriterionRevision,
		Outcome:           generated.AcceptanceOutcome(check.Outcome), Evidence: check.Evidence,
		CheckerType: generated.AcceptanceCheckCheckerType(check.Checker.Type),
		CheckedAt:   check.CheckedAt,
	}
	if check.Checker.UserID != nil {
		out.CheckedByUserID = generated.NewOptUUID(*check.Checker.UserID)
	}
	if check.Checker.Ref != "" {
		out.CheckerRef = generated.NewOptString(check.Checker.Ref)
	}
	return out
}

func projectActivityFromDomain(activity domain.ProjectActivity) generated.ProjectActivity {
	out := generated.ProjectActivity{
		ID: activity.ID, ActorID: activity.ActorID, Action: activity.Action,
		CreatedAt: activity.CreatedAt,
	}
	if activity.MilestoneID != nil {
		out.MilestoneID = generated.NewOptUUID(*activity.MilestoneID)
	}
	if activity.Reason != nil {
		out.Reason = generated.NewOptString(*activity.Reason)
	}
	if activity.OldValue != nil {
		out.OldValue = generated.NewOptString(*activity.OldValue)
	}
	if activity.NewValue != nil {
		out.NewValue = generated.NewOptString(*activity.NewValue)
	}
	if activity.AuthMethod != nil {
		out.AuthenticationMethod = generated.NewOptProjectActivityAuthenticationMethod(
			generated.ProjectActivityAuthenticationMethod(*activity.AuthMethod),
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

func optionalReason(req generated.OptLifecycleRequest) string {
	value, ok := req.Get()
	if !ok {
		return ""
	}
	return value.Reason.Or("")
}

func milestonePreconditions(
	milestoneIfMatch string,
	projectIfMatch string,
) (milestoneVersion, projectVersion int64, err error) {
	milestoneVersion, err = parseIfMatch(milestoneIfMatch)
	if err != nil {
		return 0, 0, err
	}
	projectVersion, err = parseIfMatch(projectIfMatch)
	if err != nil {
		return 0, 0, err
	}
	return milestoneVersion, projectVersion, nil
}
