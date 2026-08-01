package v1

import (
	"context"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
)

func (h *Handler) ListProjectMembers(
	ctx context.Context,
	params generated.ListProjectMembersParams,
) (generated.ListProjectMembersRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	project, err := h.Access.RequireProjectByNumber(
		ctx, params.Number, subject, application.ProjectPermissionRead,
	)
	if err != nil {
		return nil, err
	}
	memberships, err := h.Access.Memberships.List(ctx, project.Project.ID)
	if err != nil {
		return nil, err
	}
	items := make([]generated.ProjectMembership, len(memberships))
	for i, membership := range memberships {
		items[i] = projectMembershipFromDomain(membership)
	}
	return &generated.ProjectMembershipListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.ProjectMembershipList{Items: items},
	}, nil
}

func (h *Handler) AddProjectMember(
	ctx context.Context,
	req *generated.ProjectMembershipCreate,
	params generated.AddProjectMemberParams,
) (generated.AddProjectMemberRes, error) {
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
	project, err := h.Access.RequireProjectByNumber(
		ctx, params.Number, subject, application.ProjectPermissionAdmin,
	)
	if err != nil {
		return nil, err
	}
	changed, err := h.Access.Memberships.Add(
		ctx, project.Project.ID, req.UserID, domain.ProjectRole(req.Role),
		expectedVersion, actor,
	)
	if err != nil {
		return nil, err
	}
	return &generated.ProjectMembershipChangedHeaders{
		Etag:       generated.NewOptString(formatETag(changed.ProjectVersion)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.ProjectMembershipMutation{
			ProjectVersion: changed.ProjectVersion,
			Membership: generated.NewOptProjectMembership(
				projectMembershipFromDomain(changed.Membership),
			),
		},
	}, nil
}

func (h *Handler) UpdateProjectMember(
	ctx context.Context,
	req *generated.ProjectMembershipPatch,
	params generated.UpdateProjectMemberParams,
) (generated.UpdateProjectMemberRes, error) {
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
	project, err := h.Access.RequireProjectByNumber(
		ctx, params.Number, subject, application.ProjectPermissionAdmin,
	)
	if err != nil {
		return nil, err
	}
	changed, err := h.Access.Memberships.ChangeRole(
		ctx, project.Project.ID, params.UserID, domain.ProjectRole(req.Role),
		expectedVersion, actor,
	)
	if err != nil {
		return nil, err
	}
	return &generated.ProjectMembershipChangedHeaders{
		Etag:       generated.NewOptString(formatETag(changed.ProjectVersion)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.ProjectMembershipMutation{
			ProjectVersion: changed.ProjectVersion,
			Membership: generated.NewOptProjectMembership(
				projectMembershipFromDomain(changed.Membership),
			),
		},
	}, nil
}

func (h *Handler) RemoveProjectMember(
	ctx context.Context,
	params generated.RemoveProjectMemberParams,
) (generated.RemoveProjectMemberRes, error) {
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
	project, err := h.Access.RequireProjectByNumber(
		ctx, params.Number, subject, application.ProjectPermissionAdmin,
	)
	if err != nil {
		return nil, err
	}
	version, err := h.Access.Memberships.Remove(
		ctx, project.Project.ID, params.UserID, expectedVersion, actor,
	)
	if err != nil {
		return nil, err
	}
	return &generated.ProjectMembershipMutationHeaders{
		Etag:       generated.NewOptString(formatETag(version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.ProjectMembershipMutation{
			ProjectVersion: version,
		},
	}, nil
}

func projectMembershipFromDomain(membership domain.ProjectMembership) generated.ProjectMembership {
	return generated.ProjectMembership{
		ID: membership.ID, ProjectID: membership.ProjectID,
		User: userRefFromDomain(membership.User),
		Role: generated.ProjectRole(membership.Role), Active: membership.Active,
		CreatedAt: membership.CreatedAt, UpdatedAt: membership.UpdatedAt,
	}
}
