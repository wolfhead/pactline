package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/legacy/domain"
	"github.com/wolfhead/pactline/internal/legacy/store"

	"github.com/stretchr/testify/require"
)

func TestCalibrationCreateAndListByBounty(t *testing.T) {
	db := newTestDB(t)
	bs, cal := store.NewBountyStore(db), store.NewCalibrationStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.Status = domain.StatusCompleted
	b.ValueLevel = domain.ValueA
	b.Difficulty = domain.DifficultyM
	b.Completion = domain.CompletionMet
	created, err := bs.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)
	_, err = bs.Settle(ctx, created.ID, 10, time.Now().UTC())
	require.NoError(t, err)

	c, err := cal.Create(ctx, domain.Calibration{
		BountyID:        created.ID,
		Quarter:         "2026Q3",
		OriginalValue:   domain.ValueA,
		CalibratedValue: domain.ValueB,
		CalibratedScore: 6,
		Note:            "实际毛利未达预期,下调一档",
		CreatedBy:       userLeadB,
	})
	require.NoError(t, err)
	require.NotEmpty(t, c.ID)
	require.Equal(t, domain.ValueA, c.OriginalValue)
	require.Equal(t, domain.ValueB, c.CalibratedValue)
	require.InDelta(t, 6, c.CalibratedScore, 1e-9)

	// The snapshot must remain untouched: a calibration overrides, it never
	// mutates settled_score.
	got, err := bs.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, *got.SettledScore, 1e-9, "calibration must not mutate the settlement snapshot")

	list, err := cal.ListByBounty(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, c.ID, list[0].ID)
}

func TestCalibrationListByBountyEmptyWhenNone(t *testing.T) {
	db := newTestDB(t)
	bs, cal := store.NewBountyStore(db), store.NewCalibrationStore(db)
	ctx := context.Background()

	created, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	list, err := cal.ListByBounty(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, list)
}
