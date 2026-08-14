package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/wolfhead/pactline/internal/domain"
)

type RepositoryProviderClient interface {
	Provider() domain.RepositoryProvider
	ParseRepositoryURL(string) (domain.RepositoryReference, error)
	ParseCodeChangeURL(string) (domain.CodeChangeReference, error)
	ResolveRepository(
		context.Context, domain.RepositoryReference, []byte, string,
	) (domain.RepositoryIdentity, error)
	GetCodeChange(
		context.Context, string, string, domain.CodeChangeKind, int64, []byte, string,
	) (domain.CodeChange, error)
}

type RepositoryProviderRegistry struct {
	providers map[domain.RepositoryProvider]RepositoryProviderClient
}

func NewRepositoryProviderRegistry(clients ...RepositoryProviderClient) (*RepositoryProviderRegistry, error) {
	registry := &RepositoryProviderRegistry{providers: make(map[domain.RepositoryProvider]RepositoryProviderClient)}
	for _, client := range clients {
		if client == nil || !client.Provider().Valid() {
			return nil, fmt.Errorf("%w: repository provider is invalid", domain.ErrInvalidInput)
		}
		if _, duplicate := registry.providers[client.Provider()]; duplicate {
			return nil, fmt.Errorf("%w: duplicate repository provider %s", domain.ErrConflict, client.Provider())
		}
		registry.providers[client.Provider()] = client
	}
	return registry, nil
}

func (r *RepositoryProviderRegistry) Provider(provider domain.RepositoryProvider) (RepositoryProviderClient, error) {
	if r == nil {
		return nil, domain.ErrIntegrationNotConfigured
	}
	client, ok := r.providers[provider]
	if !ok {
		return nil, fmt.Errorf("%w: repository provider %s is not configured", domain.ErrIntegrationNotConfigured, provider)
	}
	return client, nil
}

func (r *RepositoryProviderRegistry) ParseRepositoryURL(
	provider domain.RepositoryProvider,
	rawURL string,
) (domain.RepositoryReference, error) {
	client, err := r.Provider(provider)
	if err != nil {
		return domain.RepositoryReference{}, err
	}
	reference, err := client.ParseRepositoryURL(rawURL)
	if err != nil {
		return domain.RepositoryReference{}, mapRepositoryProviderError(provider, err)
	}
	return reference, nil
}

func (r *RepositoryProviderRegistry) MatchRepositoryURL(rawURL string) (domain.RepositoryReference, error) {
	if r == nil || len(r.providers) == 0 {
		return domain.RepositoryReference{}, domain.ErrIntegrationNotConfigured
	}
	providers := make([]string, 0, len(r.providers))
	for provider := range r.providers {
		providers = append(providers, string(provider))
	}
	sort.Strings(providers)
	var match *domain.RepositoryReference
	for _, value := range providers {
		provider := domain.RepositoryProvider(value)
		reference, err := r.providers[provider].ParseRepositoryURL(rawURL)
		if err != nil {
			continue
		}
		if match != nil {
			return domain.RepositoryReference{}, fmt.Errorf("%w: repository URL matches more than one provider", domain.ErrConflict)
		}
		match = &reference
	}
	if match == nil {
		return domain.RepositoryReference{}, fmt.Errorf("%w: repository URL does not match a configured provider", domain.ErrInvalidInput)
	}
	return *match, nil
}

func (r *RepositoryProviderRegistry) MatchCodeChangeURL(rawURL string) (domain.CodeChangeReference, error) {
	if r == nil || len(r.providers) == 0 {
		return domain.CodeChangeReference{}, domain.ErrIntegrationNotConfigured
	}
	providers := make([]string, 0, len(r.providers))
	for provider := range r.providers {
		providers = append(providers, string(provider))
	}
	sort.Strings(providers)
	var match *domain.CodeChangeReference
	for _, value := range providers {
		client := r.providers[domain.RepositoryProvider(value)]
		reference, err := client.ParseCodeChangeURL(rawURL)
		if err != nil {
			continue
		}
		if match != nil {
			return domain.CodeChangeReference{}, fmt.Errorf("%w: code change URL matches more than one provider", domain.ErrConflict)
		}
		match = &reference
	}
	if match == nil {
		return domain.CodeChangeReference{}, fmt.Errorf("%w: code change URL does not match a configured provider", domain.ErrInvalidInput)
	}
	return *match, nil
}

func mapRepositoryProviderError(provider domain.RepositoryProvider, err error) error {
	var categorized interface{ RepositoryProviderErrorCategory() string }
	if !errors.As(err, &categorized) {
		return fmt.Errorf("%w: repository provider rejected the request", domain.ErrProviderRejected)
	}
	name := repositoryProviderName(provider)
	switch categorized.RepositoryProviderErrorCategory() {
	case "invalid_reference":
		return fmt.Errorf("%w: %s reference is invalid", domain.ErrInvalidInput, name)
	case "not_found":
		return fmt.Errorf("%w: %s resource was not found", domain.ErrNotFound, name)
	case "unauthorized":
		return fmt.Errorf("%w: %s rejected the credential", domain.ErrProviderUnauthorized, name)
	case "unreachable":
		return fmt.Errorf("%w: %s is unavailable", domain.ErrProviderUnavailable, name)
	default:
		return fmt.Errorf("%w: %s rejected the request", domain.ErrProviderRejected, name)
	}
}

func repositoryProviderName(provider domain.RepositoryProvider) string {
	switch provider {
	case domain.RepositoryProviderGitLab:
		return "GitLab"
	case domain.RepositoryProviderGitHub:
		return "GitHub"
	default:
		return "repository provider"
	}
}

func repositoryIntegrationFailureCategory(err error) string {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return "invalid_reference"
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrProviderUnauthorized):
		return "unauthorized"
	case errors.Is(err, domain.ErrProviderUnavailable):
		return "unreachable"
	case errors.Is(err, domain.ErrProviderRejected):
		return "provider_rejected"
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrVersionConflict):
		return "conflict"
	default:
		return "internal"
	}
}
