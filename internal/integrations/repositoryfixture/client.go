package repositoryfixture

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
)

const (
	SyntheticCredential = "pactline-fixture-read-token"
	GitHubOrigin        = "https://github.example.test"
	GitLabOrigin        = "https://gitlab.example.test"
	RepositoryPath      = "pactline/acceptance"
	GitHubRepositoryID  = "990001"
	GitLabRepositoryID  = "990002"
	GitHubChangeNumber  = int64(42)
	GitLabChangeNumber  = int64(43)
	GitHubChangeID      = "991042"
	GitLabChangeID      = "992043"
)

type ProviderClient interface {
	Provider() domain.RepositoryProvider
	ParseRepositoryURL(string) (domain.RepositoryReference, error)
	ParseCodeChangeURL(string) (domain.CodeChangeReference, error)
	ResolveRepository(
		context.Context, domain.RepositoryReference, []byte, string,
	) (domain.RepositoryIdentity, error)
	GetCodeChange(
		context.Context, domain.RepositoryReference, string, domain.CodeChangeKind, int64, []byte, string,
	) (domain.CodeChange, error)
}

type ErrorCategory string

const (
	ErrorInvalidReference ErrorCategory = "invalid_reference"
	ErrorNotFound         ErrorCategory = "not_found"
	ErrorUnauthorized     ErrorCategory = "unauthorized"
	ErrorProviderRejected ErrorCategory = "provider_rejected"
)

type ProviderError struct {
	Category ErrorCategory
	Err      error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("repository fixture %s", e.Category)
}

func (e *ProviderError) Unwrap() error { return e.Err }

func (e *ProviderError) RepositoryProviderErrorCategory() string { return string(e.Category) }

type Client struct {
	provider domain.RepositoryProvider
	delegate ProviderClient
	now      func() time.Time
}

func New(provider domain.RepositoryProvider, delegate ProviderClient) (*Client, error) {
	if !provider.Valid() || delegate == nil || delegate.Provider() != provider {
		return nil, fmt.Errorf("%w: fixture delegate does not match provider", domain.ErrInvalidInput)
	}
	return &Client{provider: provider, delegate: delegate, now: time.Now}, nil
}

func (c *Client) Provider() domain.RepositoryProvider { return c.provider }

func (c *Client) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	return c.delegate.ParseRepositoryURL(raw)
}

func (c *Client) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	return c.delegate.ParseCodeChangeURL(raw)
}

func (c *Client) ResolveRepository(
	ctx context.Context,
	reference domain.RepositoryReference,
	credential []byte,
	requestID string,
) (domain.RepositoryIdentity, error) {
	if !c.fixtureOrigin(reference.Origin) {
		return c.delegate.ResolveRepository(ctx, reference, credential, requestID)
	}
	if err := c.validateCredential(credential); err != nil {
		c.logResult(ctx, "resolve_repository", reference.Origin, 0, "unauthorized", requestID)
		return domain.RepositoryIdentity{}, err
	}
	if reference.Provider != c.provider {
		return domain.RepositoryIdentity{}, fixtureError(ErrorInvalidReference, "provider does not match fixture")
	}
	if reference.PathLookupKey != RepositoryPath {
		c.logResult(ctx, "resolve_repository", reference.Origin, 0, "not_found", requestID)
		return domain.RepositoryIdentity{}, fixtureError(ErrorNotFound, "repository fixture was not found")
	}
	identity := domain.RepositoryIdentity{
		ProviderRepositoryID: c.repositoryID(),
		PathWithNamespace:    RepositoryPath,
		WebURL:               reference.Origin + "/" + RepositoryPath,
		DefaultBranch:        "main",
	}
	if err := identity.Validate(); err != nil {
		return domain.RepositoryIdentity{}, fixtureError(ErrorProviderRejected, "fixture repository identity is invalid")
	}
	c.logResult(ctx, "resolve_repository", reference.Origin, 0, "confirmed", requestID)
	return identity, nil
}

func (c *Client) GetCodeChange(
	ctx context.Context,
	reference domain.RepositoryReference,
	providerRepositoryID string,
	kind domain.CodeChangeKind,
	changeNumber int64,
	credential []byte,
	requestID string,
) (domain.CodeChange, error) {
	if !c.fixtureOrigin(reference.Origin) {
		return c.delegate.GetCodeChange(
			ctx, reference, providerRepositoryID, kind, changeNumber, credential, requestID,
		)
	}
	if err := c.validateCredential(credential); err != nil {
		c.logResult(ctx, "get_code_change", reference.Origin, changeNumber, "unauthorized", requestID)
		return domain.CodeChange{}, err
	}
	if reference.Provider != c.provider || !kind.CompatibleWith(c.provider) {
		return domain.CodeChange{}, fixtureError(ErrorInvalidReference, "code change does not match fixture provider")
	}
	if reference.PathLookupKey != RepositoryPath || providerRepositoryID != c.repositoryID() ||
		changeNumber != c.changeNumber() {
		c.logResult(ctx, "get_code_change", reference.Origin, changeNumber, "not_found", requestID)
		return domain.CodeChange{}, fixtureError(ErrorNotFound, "code change fixture was not found")
	}
	now := c.now().UTC()
	change := domain.CodeChange{
		Provider: c.provider, Kind: kind,
		ProviderChangeID: c.changeID(), ChangeNumber: changeNumber,
		WebURL: c.codeChangeURL(reference.Origin),
		Observation: domain.CodeChangeObservation{
			Status: domain.CodeChangeObservationConfirmed, ObservedAt: now,
			Title: "Pactline repository fixture delivery", State: domain.CodeChangeStateOpened,
			Draft: false, SourceBranch: c.sourceBranch(), TargetBranch: "main",
			HeadSHA: c.headSHA(), ProviderUpdatedAt: now.Add(-time.Minute),
		},
	}
	if err := change.Validate(); err != nil {
		return domain.CodeChange{}, fixtureError(ErrorProviderRejected, "fixture code change is invalid")
	}
	c.logResult(ctx, "get_code_change", reference.Origin, changeNumber, "confirmed", requestID)
	return change, nil
}

func (c *Client) fixtureOrigin(origin string) bool {
	return (c.provider == domain.RepositoryProviderGitHub && origin == GitHubOrigin) ||
		(c.provider == domain.RepositoryProviderGitLab && origin == GitLabOrigin)
}

func (*Client) validateCredential(credential []byte) error {
	if subtle.ConstantTimeCompare(credential, []byte(SyntheticCredential)) != 1 {
		return fixtureError(ErrorUnauthorized, "fixture credential was rejected")
	}
	return nil
}

func (c *Client) repositoryID() string {
	if c.provider == domain.RepositoryProviderGitHub {
		return GitHubRepositoryID
	}
	return GitLabRepositoryID
}

func (c *Client) changeNumber() int64 {
	if c.provider == domain.RepositoryProviderGitHub {
		return GitHubChangeNumber
	}
	return GitLabChangeNumber
}

func (c *Client) changeID() string {
	if c.provider == domain.RepositoryProviderGitHub {
		return GitHubChangeID
	}
	return GitLabChangeID
}

func (c *Client) codeChangeURL(origin string) string {
	if c.provider == domain.RepositoryProviderGitHub {
		return fmt.Sprintf("%s/%s/pull/%d", origin, RepositoryPath, GitHubChangeNumber)
	}
	return fmt.Sprintf("%s/%s/-/merge_requests/%d", origin, RepositoryPath, GitLabChangeNumber)
}

func (c *Client) sourceBranch() string {
	if c.provider == domain.RepositoryProviderGitHub {
		return "fixture/github-delivery"
	}
	return "fixture/gitlab-delivery"
}

func (c *Client) headSHA() string {
	if c.provider == domain.RepositoryProviderGitHub {
		return "1111111111111111111111111111111111111111"
	}
	return "2222222222222222222222222222222222222222"
}

func (c *Client) logResult(
	ctx context.Context,
	operation string,
	origin string,
	changeNumber int64,
	outcome string,
	requestID string,
) {
	slog.InfoContext(ctx, "repository fixture request completed",
		"provider", c.provider,
		"operation", operation,
		"origin", origin,
		"provider_repository_id", c.repositoryID(),
		"change_number", changeNumber,
		"outcome", outcome,
		"request_id", requestID,
	)
}

func fixtureError(category ErrorCategory, message string) *ProviderError {
	return &ProviderError{Category: category, Err: errors.New(message)}
}
