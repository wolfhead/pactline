package application

import (
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompactIssueContextAlwaysIncludesOriginalResolutionRequest(t *testing.T) {
	request := domain.ThreadItem{ID: uuid.New(), Kind: domain.ThreadItemKindResolutionRequest}
	recent := []domain.ThreadItem{
		{ID: uuid.New(), Kind: domain.ThreadItemKindMessage},
		{ID: uuid.New(), Kind: domain.ThreadItemKindMessage},
	}

	withRequest := includeOriginalThreadItem(recent, request)
	require.Len(t, withRequest, 3)
	require.Equal(t, request.ID, withRequest[0].ID)

	alreadyIncluded := includeOriginalThreadItem(withRequest, request)
	require.Equal(t, withRequest, alreadyIncluded)
}
