package v1

import (
	"context"
	"fmt"
	"net/url"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/store"
)

func (h *Handler) ListProjectRepositories(
	ctx context.Context,
	params generated.ListProjectRepositoriesParams,
) (generated.ListProjectRepositoriesRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	repositories, err := h.ProjectRepositories.List(ctx, params.Number, subject)
	if err != nil {
		return nil, err
	}
	items := make([]generated.ProjectRepository, len(repositories))
	for index, repository := range repositories {
		items[index], err = projectRepositoryFromDomain(repository)
		if err != nil {
			return nil, err
		}
	}
	return &generated.ProjectRepositoryListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.ProjectRepositoryList{Items: items},
	}, nil
}

func (h *Handler) BindProjectRepository(
	ctx context.Context,
	req *generated.ProjectRepositoryBind,
	params generated.BindProjectRepositoryParams,
) (generated.BindProjectRepositoryRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.ProjectRepositories.Bind(
		ctx, params.Number, expectedVersion, req.RepositoryURL.String(), subject, operation,
	)
	if err != nil {
		return nil, err
	}
	return projectRepositoryMutationResponse(ctx, mutation)
}

func (h *Handler) UnbindProjectRepository(
	ctx context.Context,
	params generated.UnbindProjectRepositoryParams,
) (generated.UnbindProjectRepositoryRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.ProjectRepositories.Unbind(
		ctx, params.Number, expectedVersion, params.RepositoryID, subject, operation,
	)
	if err != nil {
		return nil, err
	}
	return projectRepositoryMutationResponse(ctx, mutation)
}

func projectRepositoryMutationResponse(
	ctx context.Context,
	mutation store.ProjectRepositoryMutation,
) (*generated.ProjectRepositoryChangedHeaders, error) {
	repository, err := projectRepositoryFromDomain(mutation.Repository)
	if err != nil {
		return nil, err
	}
	return &generated.ProjectRepositoryChangedHeaders{
		Etag:       generated.NewOptString(formatETag(mutation.ProjectVersion)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.ProjectRepositoryMutation{
			ProjectVersion: mutation.ProjectVersion,
			Repository:     repository,
		},
	}, nil
}

func projectRepositoryFromDomain(
	item store.ProjectRepositoryWithConnection,
) (generated.ProjectRepository, error) {
	canonicalURL, err := url.Parse(item.Connection.CanonicalWebURL)
	if err != nil {
		return generated.ProjectRepository{}, fmt.Errorf("parse canonical GitLab repository URL: %w", err)
	}
	origin, err := url.Parse(item.Connection.Origin)
	if err != nil {
		return generated.ProjectRepository{}, fmt.Errorf("parse GitLab origin: %w", err)
	}
	return generated.ProjectRepository{
		ID: item.Repository.ID, CanonicalWebURL: *canonicalURL,
		Label: item.Connection.Label, Origin: *origin,
		GitlabProjectID:   item.Connection.GitLabProjectID,
		PathWithNamespace: item.Connection.PathWithNamespace,
		DefaultBranch:     item.Connection.DefaultBranch, BoundAt: item.Repository.BoundAt,
	}, nil
}
