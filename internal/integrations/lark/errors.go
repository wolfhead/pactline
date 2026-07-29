package lark

import (
	"errors"
	"fmt"

	"github.com/wolfhead/pactline/internal/identity"
)

type ProviderError struct {
	Operation string
	Category  identity.ProviderErrorCategory
	RequestID string
	Cause     error
}

func (e *ProviderError) Error() string {
	if e.RequestID == "" {
		return fmt.Sprintf("lark %s failed (%s)", e.Operation, e.Category)
	}
	return fmt.Sprintf("lark %s failed (%s, request_id=%s)", e.Operation, e.Category, e.RequestID)
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

func (e *ProviderError) ProviderCategory() identity.ProviderErrorCategory {
	return e.Category
}

func (e *ProviderError) ProviderRequestID() string {
	return e.RequestID
}

func (e *ProviderError) Is(target error) bool {
	return target == identity.ErrProviderTransient &&
		(e.Category == identity.ProviderRateLimited || e.Category == identity.ProviderUnavailable)
}

func providerError(operation string, category identity.ProviderErrorCategory, requestID string, cause error) error {
	if cause == nil {
		cause = errors.New("provider request failed")
	}
	return &ProviderError{Operation: operation, Category: category, RequestID: requestID, Cause: cause}
}
