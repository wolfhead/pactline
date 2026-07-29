package store_test

import (
	"context"
	"testing"

	userdomain "github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/legacy/domain"
	"github.com/wolfhead/pactline/internal/legacy/store"

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
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestAnchorGetByIDMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := store.NewAnchorStore(db).GetByID(context.Background(), uuid.New())
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestAnchorDeleteMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	err := store.NewAnchorStore(db).Delete(context.Background(), uuid.New())
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

// TestAnchorListFiltersByDimensionAndLevel plants two decoys: one differing
// on both dimension and level, and one differing on level ALONE (same
// dimension, VALUE/B vs the target's VALUE/A). The dimension+level-differing
// decoy alone is not discriminating — deleting the Level clause from
// AnchorStore.List's WHERE and keeping only the Dimension filter would still
// return exactly the target row and pass, since the other decoy differs on
// dimension too. The same-dimension decoy closes that gap: it is only
// excluded if the Level filter is actually applied.
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
	b3, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b3.ID)

	target, err := as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionValue, Level: "A", BountyID: b1.ID,
	})
	require.NoError(t, err)
	_, err = as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionDifficulty, Level: "L", BountyID: b2.ID,
	})
	require.NoError(t, err)
	// Decoy differing on level alone: same VALUE dimension as target, level B.
	_, err = as.Create(ctx, domain.AnchorExample{
		Dimension: domain.AnchorDimensionValue, Level: "B", BountyID: b3.ID,
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
