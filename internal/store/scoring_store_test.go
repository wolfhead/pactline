package store_test

import (
	"context"
	"testing"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/scoring"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestSettledScoreImmuneToConstantChange is a direct check for spec §7.2:
// "changing scoring constants must not rewrite history." It settles a bounty
// using scoring.Score against the live constants, then mutates
// scoring.ValueWeights in place — simulating an Owner tuning the constants
// file after this settlement ran — and re-fetches the same bounty. The
// snapshot must read back exactly what was computed at settlement time,
// never recomputed against the new value: GetByID only ever reads the
// settled_score column, it does not call scoring.Score again, so this
// property holds by construction rather than by coincidence of the test data
// used elsewhere in this file.
func TestSettledScoreImmuneToConstantChange(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.Status = domain.StatusCompleted
	b.ValueLevel = domain.ValueA
	b.Difficulty = domain.DifficultyM
	b.Completion = domain.CompletionMet
	created, err := s.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	score, err := scoring.Score(created)
	require.NoError(t, err)
	require.InDelta(t, 10, score, 1e-9, "A(5) x M(2) x MET(1.0) x COMMITTED(1.0) = 10")

	settled, err := s.Settle(ctx, created.ID, score, time.Now().UTC())
	require.NoError(t, err)
	require.InDelta(t, 10, *settled.SettledScore, 1e-9)

	original := scoring.ValueWeights[domain.ValueA]
	scoring.ValueWeights[domain.ValueA] = 999
	t.Cleanup(func() { scoring.ValueWeights[domain.ValueA] = original })

	got, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, *got.SettledScore, 1e-9,
		"settled_score must be unaffected by a later change to the scoring constants")
}

// TestBountyLevelFieldsRoundTrip pins that value_level, difficulty and
// completion survive a Create/Update round trip, since Phase 1 never wrote
// these columns at all.
func TestBountyLevelFieldsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.ValueLevel = domain.ValueA
	created, err := s.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)
	require.Equal(t, domain.ValueA, created.ValueLevel)
	require.Empty(t, created.Difficulty)
	require.Empty(t, created.Completion)

	created.Difficulty = domain.DifficultyL
	created.Completion = domain.CompletionMet
	updated, err := s.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, domain.ValueA, updated.ValueLevel)
	require.Equal(t, domain.DifficultyL, updated.Difficulty)
	require.Equal(t, domain.CompletionMet, updated.Completion)

	got, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ValueA, got.ValueLevel)
	require.Equal(t, domain.DifficultyL, got.Difficulty)
	require.Equal(t, domain.CompletionMet, got.Completion)
}

// TestPhase1RecordsSurviveWithNullLevels pins the task's explicit
// requirement: "Every Phase 1 record has null levels and must survive this
// untouched." A bounty created without any of the three levels must read
// back with all three empty, not some invented zero value that looks like a
// real grade.
func TestPhase1RecordsSurviveWithNullLevels(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	created, err := s.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	require.Empty(t, created.ValueLevel)
	require.Empty(t, created.Difficulty)
	require.Empty(t, created.Completion)
	require.Nil(t, created.SettledScore)
	require.Nil(t, created.SettledAt)

	got, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.ValueLevel)
	require.Empty(t, got.Difficulty)
	require.Empty(t, got.Completion)
	require.Nil(t, got.SettledScore)
	require.Nil(t, got.SettledAt)
}

// TestSettleWritesSnapshotAndIsIdempotentlySkippable pins the settlement
// contract: Settle writes settled_score/settled_at exactly once, and a
// second call against the same already-settled row must fail with
// domain.ErrAlreadySettled rather than silently recomputing — this is the
// database-level backstop behind spec §7.2's "never recompute" rule.
func TestSettleWritesSnapshotAndIsIdempotentlySkippable(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.Status = domain.StatusCompleted
	b.ValueLevel = domain.ValueA
	b.Difficulty = domain.DifficultyM
	b.Completion = domain.CompletionMet
	created, err := s.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	now := time.Now().UTC()
	settled, err := s.Settle(ctx, created.ID, 10, now)
	require.NoError(t, err)
	require.NotNil(t, settled.SettledScore)
	require.InDelta(t, 10, *settled.SettledScore, 1e-9)
	require.NotNil(t, settled.SettledAt)

	_, err = s.Settle(ctx, created.ID, 999, time.Now().UTC())
	require.ErrorIs(t, err, domain.ErrAlreadySettled)

	// The snapshot must be untouched by the rejected second call.
	got, err := s.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.InDelta(t, 10, *got.SettledScore, 1e-9)
}

// TestUpdateNeverTouchesSettledSnapshot pins that Update — the general write
// path used by transition, amend, and the value-level/difficulty endpoints —
// cannot clobber a settlement snapshot, mirroring the existing
// TestUpdateSponsorIDIsImmutable guard for sponsor_id. Without this, a
// completion edit made after settlement (e.g. via the steward amend channel)
// could silently null out settled_score just by round-tripping the row.
func TestUpdateNeverTouchesSettledSnapshot(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	b := newBounty(userPM)
	b.Status = domain.StatusCompleted
	b.ValueLevel = domain.ValueA
	b.Difficulty = domain.DifficultyM
	b.Completion = domain.CompletionMet
	created, err := s.Create(ctx, b)
	require.NoError(t, err)
	cleanupBounties(t, db, created.ID)

	settled, err := s.Settle(ctx, created.ID, 10, time.Now().UTC())
	require.NoError(t, err)

	// Attempt to blank the snapshot out via a normal Update call.
	settled.SettledScore = nil
	settled.SettledAt = nil
	settled.Retrospective = "unrelated edit"
	updated, err := s.Update(ctx, settled)
	require.NoError(t, err)
	require.NotNil(t, updated.SettledScore, "Update must not be able to touch settled_score")
	require.InDelta(t, 10, *updated.SettledScore, 1e-9)
	require.NotNil(t, updated.SettledAt, "Update must not be able to touch settled_at")
}

// TestListFiltersByCompletedAtRange pins the CompletedFrom/CompletedTo
// filter that backs settlement's "settle a period" scan.
func TestListFiltersByCompletedAtRange(t *testing.T) {
	db := newTestDB(t)
	s := store.NewBountyStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	inRange := now.Add(-15 * 24 * time.Hour)
	beforeRange := now.Add(-60 * 24 * time.Hour)
	afterRange := now.Add(24 * time.Hour)

	mkCompleted := func(completedAt time.Time) uuid.UUID {
		b := newBounty(userPM)
		b.Status = domain.StatusCompleted
		b.CompletedAt = &completedAt
		created, err := s.Create(ctx, b)
		require.NoError(t, err)
		cleanupBounties(t, db, created.ID)
		return created.ID
	}

	inID := mkCompleted(inRange)
	mkCompleted(beforeRange)
	mkCompleted(afterRange)

	from := now.Add(-30 * 24 * time.Hour)
	to := now
	got, err := s.List(ctx, store.BountyFilter{
		Statuses:      []domain.Status{domain.StatusCompleted},
		CompletedFrom: &from,
		CompletedTo:   &to,
	})
	require.NoError(t, err)
	ids := make([]uuid.UUID, len(got))
	for i, b := range got {
		ids[i] = b.ID
	}
	require.Contains(t, ids, inID)
	require.Len(t, got, 1, "only the bounty completed inside [from, to] must be returned")
}
