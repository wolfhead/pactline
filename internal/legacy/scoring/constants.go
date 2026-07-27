// Package scoring computes a bounty's settlement score from its graded
// levels. It performs no IO — pure computation over domain values, exactly
// like internal/domain, kept as its own package because spec §9 names it
// explicitly (internal/scoring/constants.go) as the one place these numbers
// live.
package scoring

import "bountyboard/internal/legacy/domain"

// The constants below are the whole of Phase 2's tuning surface. Spec §13
// explicitly rejected building a configuration layer for them — Owner
// revisits these numbers directly, in code, each phase's feedback loop, and
// a config layer would just be one more abstraction the next maintainer has
// to read through. Tune by editing this file; nothing else needs to change.
//
// Every value below is quoted verbatim from spec §7.1. Do not add precision:
// spec §13 also rejected a continuous score, because multiplying several
// subjective estimates only amplifies their error, and precise numbers
// invite arguing over decimals.

// ValueWeights is the sponsor-set value level's weight.
var ValueWeights = map[domain.ValueLevel]float64{
	domain.ValueS: 8,
	domain.ValueA: 5,
	domain.ValueB: 3,
	domain.ValueC: 1,
}

// DifficultyWeights is the tech-lead-set difficulty level's weight.
var DifficultyWeights = map[domain.Difficulty]float64{
	domain.DifficultyXS: 0.5,
	domain.DifficultyS:  1,
	domain.DifficultyM:  2,
	domain.DifficultyL:  3.5,
	domain.DifficultyXL: 6,
}

// CompletionFactors is the sponsor-set completion grade's factor. Never
// applied to ABANDONED bounties — see AbandonedExploratoryFactor.
var CompletionFactors = map[domain.Completion]float64{
	domain.CompletionExceeded: 1.2,
	domain.CompletionMet:      1.0,
	domain.CompletionPartial:  0.6,
	domain.CompletionMissed:   0.2,
}

// CommitmentFactors is the commitment type's factor.
var CommitmentFactors = map[domain.Commitment]float64{
	domain.CommitmentCommitted:   1.0,
	domain.CommitmentExploratory: 0.7,
}

// AbandonedExploratoryFactor is spec §7.1.1's fixed multiplier applied
// instead of a completion factor when an EXPLORATORY bounty is abandoned:
// value x difficulty x 0.4 x 0.7. The two factors (0.4 here, 0.7 from
// CommitmentFactors) are deliberately stacked rather than combined into one
// number — together they land around 40% of what the same bounty would have
// scored on a successful committed delivery, which is the mechanism's "a
// failure gives partial credit" line made concrete.
const AbandonedExploratoryFactor = 0.4

// AbandonedCommittedScore is spec §7.1.1's fixed score for a COMMITTED
// bounty that ends ABANDONED: zero. A promise that was time-critical and
// then not delivered earns nothing, regardless of value or difficulty.
const AbandonedCommittedScore = 0
