package store_test

import (
	"context"
	"testing"
	"time"

	userdomain "github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/legacy/domain"
	"github.com/wolfhead/pactline/internal/legacy/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	userPM    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userLeadB = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	userEngC  = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	userEngD  = uuid.MustParse("00000000-0000-0000-0000-000000000004")
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
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestUpdatePersistsClaim(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	created, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	// created_at and the initial updated_at are both set by the same now()
	// call inside Create's single INSERT, so they are identical. Sleeping
	// here before Update ensures its own now() call lands on a distinct wall
	// clock value, so the strictly-after assertion below can only pass if
	// Update actually refreshes updated_at, not by coincidence of two now()
	// calls landing in the same instant.
	time.Sleep(2 * time.Millisecond)

	created.Status = domain.StatusClaimed
	created.ClaimedBy = &userEngC
	updated, err := s.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, domain.StatusClaimed, updated.Status)
	require.NotNil(t, updated.ClaimedBy)
	require.Equal(t, userEngC, *updated.ClaimedBy)
	require.True(t, updated.UpdatedAt.After(created.UpdatedAt),
		"Update must refresh updated_at: got %s, want strictly after %s", updated.UpdatedAt, created.UpdatedAt)
}

// TestUpdateSponsorIDIsImmutable pins the intended behaviour that sponsor_id
// is never written by Update: it is who opened the bounty, a fact about its
// origin, not a mutable attribute. See the comment on Update in
// bounty_store.go.
func TestUpdateSponsorIDIsImmutable(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	created, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	created.SponsorID = userLeadB
	updated, err := s.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, userPM, updated.SponsorID, "sponsor_id must not change via Update")
}

func TestListFiltersByStatusAndTag(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	open := newBounty(userPM)
	open.Status = domain.StatusOpen
	createdOpen, err := s.Create(ctx, open)
	require.NoError(t, err)
	cleanupBounties(t, db, createdOpen.ID)

	platform := newBounty(userPM)
	platform.Status = domain.StatusOpen
	platform.BusinessLines = []domain.BusinessLine{{Tag: domain.BusinessTagPlatform, Weight: 1}}
	createdPlatform, err := s.Create(ctx, platform)
	require.NoError(t, err)
	cleanupBounties(t, db, createdPlatform.ID)

	draft := newBounty(userPM)
	createdDraft, err := s.Create(ctx, draft)
	require.NoError(t, err)
	cleanupBounties(t, db, createdDraft.ID)

	opens, err := s.List(ctx, store.BountyFilter{Statuses: []domain.Status{domain.StatusOpen}})
	require.NoError(t, err)
	require.Len(t, opens, 2)

	platforms, err := s.List(ctx, store.BountyFilter{BusinessTag: domain.BusinessTagPlatform})
	require.NoError(t, err)
	require.Len(t, platforms, 1)
}

// TestListByClaimedBy plants a decoy row claimed by a different user, like
// every sibling filter test in this file: without it, a single-row fixture
// would still pass even with the claimed_by filter deleted from the query.
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

	decoy := newBounty(userPM)
	decoy.Status = domain.StatusClaimed
	decoy.ClaimedBy = &userEngC
	createdDecoy, err := s.Create(ctx, decoy)
	require.NoError(t, err)
	cleanupBounties(t, db, createdDecoy.ID)

	mine, err := s.List(ctx, store.BountyFilter{ClaimedBy: &userEngD})
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, created.ID, mine[0].ID)
}

func TestListFiltersByType(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	plan := newBounty(userPM)
	plan.Type = domain.BountyTypePlan
	createdPlan, err := s.Create(ctx, plan)
	require.NoError(t, err)
	cleanupBounties(t, db, createdPlan.ID)

	delivery := newBounty(userPM)
	delivery.Type = domain.BountyTypeDelivery
	createdDelivery, err := s.Create(ctx, delivery)
	require.NoError(t, err)
	cleanupBounties(t, db, createdDelivery.ID)

	planType := domain.BountyTypePlan
	plans, err := s.List(ctx, store.BountyFilter{Type: &planType})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, createdPlan.ID, plans[0].ID)

	deliveryType := domain.BountyTypeDelivery
	deliveries, err := s.List(ctx, store.BountyFilter{Type: &deliveryType})
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Equal(t, createdDelivery.ID, deliveries[0].ID)
}

func TestListFiltersBySponsorID(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	mine, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, mine.ID)

	other, err := s.Create(ctx, newBounty(userLeadB))
	require.NoError(t, err)
	cleanupBounties(t, db, other.ID)

	sponsor := userPM
	got, err := s.List(ctx, store.BountyFilter{SponsorID: &sponsor})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, mine.ID, got[0].ID)
}

func TestListOrderByCompletedAt(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	tOldest := now.Add(-3 * time.Hour)
	tMiddle := now.Add(-2 * time.Hour)
	tNewest := now.Add(-1 * time.Hour)

	oldest := newBounty(userPM)
	oldest.CompletedAt = &tOldest
	createdOldest, err := s.Create(ctx, oldest)
	require.NoError(t, err)
	cleanupBounties(t, db, createdOldest.ID)

	middle := newBounty(userPM)
	middle.CompletedAt = &tMiddle
	createdMiddle, err := s.Create(ctx, middle)
	require.NoError(t, err)
	cleanupBounties(t, db, createdMiddle.ID)

	newest := newBounty(userPM)
	newest.CompletedAt = &tNewest
	createdNewest, err := s.Create(ctx, newest)
	require.NoError(t, err)
	cleanupBounties(t, db, createdNewest.ID)

	noCompletion := newBounty(userPM)
	createdNoCompletion, err := s.Create(ctx, noCompletion)
	require.NoError(t, err)
	cleanupBounties(t, db, createdNoCompletion.ID)

	got, err := s.List(ctx, store.BountyFilter{OrderByCompletedAt: true})
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, createdNewest.ID, got[0].ID, "newest completed_at must sort first")
	require.Equal(t, createdMiddle.ID, got[1].ID)
	require.Equal(t, createdOldest.ID, got[2].ID)
	require.Equal(t, createdNoCompletion.ID, got[3].ID, "nil completed_at must sort last")
}

// TestListCombinesFiltersWithAnd guards against an implementation that joins
// filter clauses with OR, or silently drops all but one active filter. Three
// bounties are set up so that each individual filter (Type or SponsorID)
// alone matches more than one row, but the two together intersect to exactly
// one: this makes both the OR-join bug and the dropped-filter bug visible as
// a wrong count/row, not merely a coincidentally-passing test.
func TestListCombinesFiltersWithAnd(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	planPM := newBounty(userPM)
	planPM.Type = domain.BountyTypePlan
	createdPlanPM, err := s.Create(ctx, planPM)
	require.NoError(t, err)
	cleanupBounties(t, db, createdPlanPM.ID)

	deliveryPM := newBounty(userPM)
	deliveryPM.Type = domain.BountyTypeDelivery
	createdDeliveryPM, err := s.Create(ctx, deliveryPM)
	require.NoError(t, err)
	cleanupBounties(t, db, createdDeliveryPM.ID)

	planLeadB := newBounty(userLeadB)
	planLeadB.Type = domain.BountyTypePlan
	createdPlanLeadB, err := s.Create(ctx, planLeadB)
	require.NoError(t, err)
	cleanupBounties(t, db, createdPlanLeadB.ID)

	planType := domain.BountyTypePlan
	sponsor := userPM
	got, err := s.List(ctx, store.BountyFilter{Type: &planType, SponsorID: &sponsor})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, createdPlanPM.ID, got[0].ID)
}
