package scoring_test

import (
	"testing"

	"github.com/wolfhead/pactline/internal/legacy/domain"
	"github.com/wolfhead/pactline/internal/legacy/scoring"

	"github.com/stretchr/testify/require"
)

func completed(value domain.ValueLevel, difficulty domain.Difficulty, completion domain.Completion, commitment domain.Commitment) domain.Bounty {
	return domain.Bounty{
		Status:     domain.StatusCompleted,
		ValueLevel: value,
		Difficulty: difficulty,
		Completion: completion,
		Commitment: commitment,
	}
}

// TestScoreEveryValueDifficultyCombination pins the full 4x4x5 grid at
// MET/COMMITTED (completion=1.0, commitment=1.0, so score == value weight x
// difficulty weight exactly), with every expected number transcribed
// independently from spec §7.1's table rather than derived from
// scoring.ValueWeights/DifficultyWeights themselves — a typo in either
// constants map must fail here, not just agree with itself.
func TestScoreEveryValueDifficultyCombination(t *testing.T) {
	cases := []struct {
		value      domain.ValueLevel
		difficulty domain.Difficulty
		want       float64
	}{
		{domain.ValueS, domain.DifficultyXS, 4},
		{domain.ValueS, domain.DifficultyS, 8},
		{domain.ValueS, domain.DifficultyM, 16},
		{domain.ValueS, domain.DifficultyL, 28},
		{domain.ValueS, domain.DifficultyXL, 48},
		{domain.ValueA, domain.DifficultyXS, 2.5},
		{domain.ValueA, domain.DifficultyS, 5},
		{domain.ValueA, domain.DifficultyM, 10},
		{domain.ValueA, domain.DifficultyL, 17.5},
		{domain.ValueA, domain.DifficultyXL, 30},
		{domain.ValueB, domain.DifficultyXS, 1.5},
		{domain.ValueB, domain.DifficultyS, 3},
		{domain.ValueB, domain.DifficultyM, 6},
		{domain.ValueB, domain.DifficultyL, 10.5},
		{domain.ValueB, domain.DifficultyXL, 18},
		{domain.ValueC, domain.DifficultyXS, 0.5},
		{domain.ValueC, domain.DifficultyS, 1},
		{domain.ValueC, domain.DifficultyM, 2},
		{domain.ValueC, domain.DifficultyL, 3.5},
		{domain.ValueC, domain.DifficultyXL, 6},
	}
	for _, tc := range cases {
		t.Run(string(tc.value)+"_"+string(tc.difficulty), func(t *testing.T) {
			b := completed(tc.value, tc.difficulty, domain.CompletionMet, domain.CommitmentCommitted)
			got, err := scoring.Score(b)
			require.NoError(t, err)
			require.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

// TestScoreCompletionFactors fixes value=A (5) and difficulty=M (2), so the
// base is 10, and pins each completion factor against an independently
// transcribed expected total.
func TestScoreCompletionFactors(t *testing.T) {
	cases := []struct {
		completion domain.Completion
		want       float64
	}{
		{domain.CompletionExceeded, 12},
		{domain.CompletionMet, 10},
		{domain.CompletionPartial, 6},
		{domain.CompletionMissed, 2},
	}
	for _, tc := range cases {
		t.Run(string(tc.completion), func(t *testing.T) {
			b := completed(domain.ValueA, domain.DifficultyM, tc.completion, domain.CommitmentCommitted)
			got, err := scoring.Score(b)
			require.NoError(t, err)
			require.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

// TestScoreCommitmentFactor fixes value=A, difficulty=M, completion=MET (base
// 10) and pins the commitment factor's effect on a successfully completed
// bounty (the non-abandoned branch; the abandoned branch has its own test
// below since it does not go through CompletionFactors at all).
func TestScoreCommitmentFactor(t *testing.T) {
	committed := completed(domain.ValueA, domain.DifficultyM, domain.CompletionMet, domain.CommitmentCommitted)
	got, err := scoring.Score(committed)
	require.NoError(t, err)
	require.InDelta(t, 10, got, 1e-9)

	exploratory := completed(domain.ValueA, domain.DifficultyM, domain.CompletionMet, domain.CommitmentExploratory)
	got, err = scoring.Score(exploratory)
	require.NoError(t, err)
	require.InDelta(t, 7, got, 1e-9)
}

// TestScoreAbandonedExploratory pins spec §7.1.1's abandoned-exploratory
// formula exactly: value x difficulty x 0.4 x 0.7. Using value=A (5) and
// difficulty=M (2): 5 x 2 x 0.4 x 0.7 = 2.8. Crucially, Completion is left
// unset (zero value) to prove the completion factor is never consulted on
// this branch — a bounty in ABANDONED never carries one.
func TestScoreAbandonedExploratory(t *testing.T) {
	b := domain.Bounty{
		Status:     domain.StatusAbandoned,
		ValueLevel: domain.ValueA,
		Difficulty: domain.DifficultyM,
		Commitment: domain.CommitmentExploratory,
	}
	got, err := scoring.Score(b)
	require.NoError(t, err)
	require.InDelta(t, 2.8, got, 1e-9)
}

// TestScoreAbandonedCommitted pins spec §7.1.1's other half: a COMMITTED
// bounty that is abandoned scores exactly zero, regardless of how high its
// value or difficulty were.
func TestScoreAbandonedCommitted(t *testing.T) {
	b := domain.Bounty{
		Status:     domain.StatusAbandoned,
		ValueLevel: domain.ValueS,
		Difficulty: domain.DifficultyXL,
		Commitment: domain.CommitmentCommitted,
	}
	got, err := scoring.Score(b)
	require.NoError(t, err)
	require.InDelta(t, 0, got, 1e-9)
}

// TestScoreMissingLevelsAreUnscorable pins the "never invent a default"
// rule: a terminal bounty missing any level it needs must return
// domain.ErrUnscorable, not a silently wrong number like zero used as a
// stand-in for "unset".
func TestScoreMissingLevelsAreUnscorable(t *testing.T) {
	t.Run("missing value_level", func(t *testing.T) {
		b := completed("", domain.DifficultyM, domain.CompletionMet, domain.CommitmentCommitted)
		_, err := scoring.Score(b)
		require.ErrorIs(t, err, domain.ErrUnscorable)
	})
	t.Run("missing difficulty", func(t *testing.T) {
		b := completed(domain.ValueA, "", domain.CompletionMet, domain.CommitmentCommitted)
		_, err := scoring.Score(b)
		require.ErrorIs(t, err, domain.ErrUnscorable)
	})
	t.Run("missing completion on a completed bounty", func(t *testing.T) {
		b := completed(domain.ValueA, domain.DifficultyM, "", domain.CommitmentCommitted)
		_, err := scoring.Score(b)
		require.ErrorIs(t, err, domain.ErrUnscorable)
	})
	t.Run("a Phase 1 record with every level null must be unscorable, not defaulted", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusCompleted, Commitment: domain.CommitmentCommitted}
		_, err := scoring.Score(b)
		require.ErrorIs(t, err, domain.ErrUnscorable)
	})
	t.Run("missing completion on an abandoned bounty is fine: it is not consulted", func(t *testing.T) {
		b := domain.Bounty{
			Status:     domain.StatusAbandoned,
			ValueLevel: domain.ValueA,
			Difficulty: domain.DifficultyM,
			Commitment: domain.CommitmentExploratory,
		}
		_, err := scoring.Score(b)
		require.NoError(t, err)
	})
}

// TestScoreWithValueLevelDoesNotMutateInput pins that ScoreWithValueLevel is
// a pure substitution: it must not mutate the caller's bounty value, only
// compute as if it were different.
func TestScoreWithValueLevelDoesNotMutateInput(t *testing.T) {
	b := completed(domain.ValueA, domain.DifficultyM, domain.CompletionMet, domain.CommitmentCommitted)
	got, err := scoring.ScoreWithValueLevel(b, domain.ValueS)
	require.NoError(t, err)
	require.InDelta(t, 16, got, 1e-9, "S x M x MET x COMMITTED = 8 x 2 = 16")
	require.Equal(t, domain.ValueA, b.ValueLevel, "the original bounty value must be untouched")

	original, err := scoring.Score(b)
	require.NoError(t, err)
	require.InDelta(t, 10, original, 1e-9, "A x M x MET x COMMITTED = 5 x 2 = 10, unaffected by the call above")
}
