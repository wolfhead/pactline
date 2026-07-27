package scoring

import (
	"fmt"

	"bountyboard/internal/domain"
)

// Score computes a terminal bounty's settlement score from spec §7.1's
// formula: value weight x difficulty weight x completion factor x commitment
// factor. It returns an error wrapping domain.ErrUnscorable, naming the
// missing or unrecognized field, when the bounty lacks a level it needs.
// Callers (settlement, calibration) must never invent a default for a
// missing level — a grade nobody gave would be fiction in the archive — they
// must skip the record and surface this error's text.
//
// ABANDONED bounties bypass the completion factor entirely, per spec
// §7.1.1: they were never accepted, so there is no completion grade to read.
// EXPLORATORY scores value x difficulty x AbandonedExploratoryFactor x its
// own commitment factor; COMMITTED scores AbandonedCommittedScore (zero) —
// a time-critical promise that was not kept earns nothing, regardless of
// value or difficulty.
func Score(b domain.Bounty) (float64, error) {
	valueWeight, ok := ValueWeights[b.ValueLevel]
	if !ok {
		return 0, fmt.Errorf("%w: missing or unknown value_level %q", domain.ErrUnscorable, b.ValueLevel)
	}
	difficultyWeight, ok := DifficultyWeights[b.Difficulty]
	if !ok {
		return 0, fmt.Errorf("%w: missing or unknown difficulty %q", domain.ErrUnscorable, b.Difficulty)
	}
	commitmentFactor, ok := CommitmentFactors[b.Commitment]
	if !ok {
		return 0, fmt.Errorf("%w: missing or unknown commitment %q", domain.ErrUnscorable, b.Commitment)
	}

	if b.Status == domain.StatusAbandoned {
		if b.Commitment == domain.CommitmentCommitted {
			return AbandonedCommittedScore, nil
		}
		return valueWeight * difficultyWeight * AbandonedExploratoryFactor * commitmentFactor, nil
	}

	completionFactor, ok := CompletionFactors[b.Completion]
	if !ok {
		return 0, fmt.Errorf("%w: missing or unknown completion %q", domain.ErrUnscorable, b.Completion)
	}
	return valueWeight * difficultyWeight * completionFactor * commitmentFactor, nil
}

// ScoreWithValueLevel scores b as if its value level were level instead of
// b.ValueLevel, leaving every other field untouched. This backs calibration
// (spec §4.6): comparing what a bounty actually settled at against what it
// would have settled at under a corrected value level, without mutating the
// bounty or its settlement snapshot.
func ScoreWithValueLevel(b domain.Bounty, level domain.ValueLevel) (float64, error) {
	b.ValueLevel = level
	return Score(b)
}
