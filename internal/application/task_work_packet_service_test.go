package application

import (
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompactIssueContextAlwaysIncludesOriginalResolutionRequest(t *testing.T) {
	request := domain.ThreadItem{ID: uuid.New(), Kind: domain.ThreadItemKindResolutionRequest}
	firstRecent := domain.ThreadItem{ID: uuid.New(), Kind: domain.ThreadItemKindMessage}
	secondRecent := domain.ThreadItem{ID: uuid.New(), Kind: domain.ThreadItemKindMessage}
	recentWithoutRequest := []domain.ThreadItem{
		firstRecent,
		secondRecent,
	}

	withRequest := includeOriginalThreadItem(recentWithoutRequest, request)
	require.Equal(t, []domain.ThreadItem{request, firstRecent, secondRecent}, withRequest)

	alreadyIncluded := includeOriginalThreadItem(withRequest, request)
	require.Equal(t, withRequest, alreadyIncluded)

	requestOutOfOrder := includeOriginalThreadItem(
		[]domain.ThreadItem{firstRecent, request, secondRecent},
		request,
	)
	require.Equal(t, []domain.ThreadItem{request, firstRecent, secondRecent}, requestOutOfOrder)
}
