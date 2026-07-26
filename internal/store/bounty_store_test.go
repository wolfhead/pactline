package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	userPM   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userEngC = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	userEngD = uuid.MustParse("00000000-0000-0000-0000-000000000004")
)

func newBounty(sponsor uuid.UUID) domain.Bounty {
	return domain.Bounty{
		Type:       domain.BountyTypeDelivery,
		Title:      "竞价链路降延迟",
		Goal:       "把 P99 从 80ms 降到 45ms",
		Visibility: domain.VisibilityPublic,
		Commitment: domain.CommitmentCommitted,
		Status:     domain.StatusDraft,
		SponsorID:  sponsor,
		BusinessLines: []domain.BusinessLine{
			{Tag: "DSP", Weight: 0.7},
			{Tag: "ADX", Weight: 0.3},
		},
	}
}

// cleanupBounties deletes the given bounty rows once the test finishes.
//
// The suite runs against a shared, already-migrated database (see
// docker-compose.yml: the postgres container is not recreated between
// `make test` invocations, so rows left behind by one run are still present
// on the next). Several tests below assert exact result counts from List,
// which would become flaky across repeated runs without this: this helper
// keeps each test responsible for the rows it created instead of weakening
// those assertions.
func cleanupBounties(t *testing.T, db *store.DB, ids ...uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM bounties WHERE id = ANY($1)`, ids)
		require.NoError(t, err)
	})
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	created, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
	cleanupBounties(t, db, created.ID)

	got, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "竞价链路降延迟", got.Title)
	require.Len(t, got.BusinessLines, 2)
	require.Equal(t, "DSP", got.BusinessLines[0].Tag)
	require.InDelta(t, 0.7, got.BusinessLines[0].Weight, 1e-9)
}

func TestGetByIDMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := store.NewBountyStore(db).GetByID(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestUpdatePersistsClaim(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	created, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	created.Status = domain.StatusClaimed
	created.ClaimedBy = &userEngC
	updated, err := s.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, domain.StatusClaimed, updated.Status)
	require.NotNil(t, updated.ClaimedBy)
	require.Equal(t, userEngC, *updated.ClaimedBy)
	require.True(t, updated.UpdatedAt.After(created.CreatedAt) || updated.UpdatedAt.Equal(created.CreatedAt))
}

func TestListFiltersByStatusAndTag(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	open := newBounty(userPM)
	open.Status = domain.StatusOpen
	createdOpen, err := s.Create(ctx, open)
	require.NoError(t, err)

	platform := newBounty(userPM)
	platform.Status = domain.StatusOpen
	platform.BusinessLines = []domain.BusinessLine{{Tag: domain.BusinessTagPlatform, Weight: 1}}
	createdPlatform, err := s.Create(ctx, platform)
	require.NoError(t, err)

	draft := newBounty(userPM)
	createdDraft, err := s.Create(ctx, draft)
	require.NoError(t, err)

	cleanupBounties(t, db, createdOpen.ID, createdPlatform.ID, createdDraft.ID)

	opens, err := s.List(ctx, store.BountyFilter{Statuses: []domain.Status{domain.StatusOpen}})
	require.NoError(t, err)
	require.Len(t, opens, 2)

	platforms, err := s.List(ctx, store.BountyFilter{BusinessTag: domain.BusinessTagPlatform})
	require.NoError(t, err)
	require.Len(t, platforms, 1)
}

func TestListByClaimedBy(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.Status = domain.StatusClaimed
	b.ClaimedBy = &userEngD
	created, err := s.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	mine, err := s.List(ctx, store.BountyFilter{ClaimedBy: &userEngD})
	require.NoError(t, err)
	require.Len(t, mine, 1)
}
