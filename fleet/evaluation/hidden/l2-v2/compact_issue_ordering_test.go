package application

import (
	"testing"

	"github.com/google/uuid"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestFleetHiddenOriginalResolutionRequestIsFirstAndUnique(t *testing.T) {
	t.Parallel()
	original := domain.ThreadItem{ID: uuid.New()}
	firstRecent := domain.ThreadItem{ID: uuid.New()}
	secondRecent := domain.ThreadItem{ID: uuid.New()}

	missing := includeOriginalThreadItem([]domain.ThreadItem{firstRecent}, original)
	require.Equal(t, []uuid.UUID{original.ID, firstRecent.ID}, []uuid.UUID{missing[0].ID, missing[1].ID})

	present := includeOriginalThreadItem([]domain.ThreadItem{firstRecent, original, secondRecent}, original)
	require.Len(t, present, 3)
	require.Equal(t, []uuid.UUID{original.ID, firstRecent.ID, secondRecent.ID}, []uuid.UUID{present[0].ID, present[1].ID, present[2].ID})
}
