package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAnchorCreateGetUpdateDelete(t *testing.T) {
	db := newTestDB(t)
	bs, as := store.NewBountyStore(db), store.NewAnchorStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	created, err := as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionValue,
		Level:     "A",
		BountyID:  b.ID,
		Note:      "去年 Q3 的竞价延迟优化,标杆 A 档",
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)

	got, err := as.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "A", got.Level)
	require.Equal(t, domain.AnchorDimensionValue, got.Dimension)

	got.Level = "S"
	got.Note = "重新定档为 S"
	updated, err := as.Update(ctx, got)
	require.NoError(t, err)
	require.Equal(t, "S", updated.Level)
	require.Equal(t, "重新定档为 S", updated.Note)

	require.NoError(t, as.Delete(ctx, created.ID))
	_, err = as.GetByID(ctx, created.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAnchorGetByIDMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := store.NewAnchorStore(db).GetByID(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAnchorDeleteMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	err := store.NewAnchorStore(db).Delete(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// TestAnchorListFiltersByDimensionAndLevel plants a decoy at a different
// dimension/level so the filter's WHERE clause is actually exercised, not
// merely a single-row fixture that would pass even with the filter deleted.
func TestAnchorListFiltersByDimensionAndLevel(t *testing.T) {
	db := newTestDB(t)
	bs, as := store.NewBountyStore(db), store.NewAnchorStore(db)
	ctx := context.Background()

	b1, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b1.ID)
	b2, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b2.ID)

	target, err := as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionValue, Level: "A", BountyID: b1.ID,
	})
	require.NoError(t, err)
	_, err = as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionDifficulty, Level: "L", BountyID: b2.ID,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = as.Delete(context.Background(), target.ID)
	})

	got, err := as.List(ctx, store.AnchorFilter{Dimension: domain.AnchorDimensionValue, Level: "A"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, target.ID, got[0].ID)
}
