package domain_test

import (
	"testing"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateTransition(t *testing.T) {
	cases := []struct {
		name    string
		from    domain.Status
		to      domain.Status
		retro   string
		wantErr error
	}{
		{"draft to open", domain.StatusDraft, domain.StatusOpen, "", nil},
		{"open to claimed", domain.StatusOpen, domain.StatusClaimed, "", nil},
		{"claimed to delivered", domain.StatusClaimed, domain.StatusDelivered, "", nil},
		{"delivered to completed", domain.StatusDelivered, domain.StatusCompleted, "", nil},
		{"claimed back to open", domain.StatusClaimed, domain.StatusOpen, "", nil},
		{"draft straight to completed", domain.StatusDraft, domain.StatusCompleted, "", domain.ErrInvalidTransition},
		{"completed is terminal", domain.StatusCompleted, domain.StatusOpen, "", domain.ErrInvalidTransition},
		{"abandoned is terminal", domain.StatusAbandoned, domain.StatusOpen, "", domain.ErrInvalidTransition},
		{"abandon without retrospective", domain.StatusClaimed, domain.StatusAbandoned, "", domain.ErrRetrospectiveRequired},
		{"abandon with retrospective", domain.StatusClaimed, domain.StatusAbandoned, "上游依赖未就绪", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := domain.Bounty{Status: tc.from, Retrospective: tc.retro}
			err := domain.ValidateTransition(b, tc.to)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestValidateTransitionMatrix exercises every from/to pair over all six
// statuses (36 combinations), asserting the allowed set exactly against a
// matrix transcribed independently from spec §5's lifecycle diagram — not
// from domain.allowedTransitions itself, so this cannot degrade into a
// tautology. The prior table only covered two invalid edges plus the happy
// path; a typo in the production transition map (e.g. an edge added or
// removed) would not necessarily be caught by that sparse coverage but must
// be caught here.
func TestValidateTransitionMatrix(t *testing.T) {
	statuses := []domain.Status{
		domain.StatusDraft, domain.StatusOpen, domain.StatusClaimed,
		domain.StatusDelivered, domain.StatusCompleted, domain.StatusAbandoned,
	}
	allowed := map[domain.Status]map[domain.Status]bool{
		domain.StatusDraft: {
			domain.StatusOpen: true, domain.StatusAbandoned: true,
		},
		domain.StatusOpen: {
			domain.StatusDraft: true, domain.StatusClaimed: true, domain.StatusAbandoned: true,
		},
		domain.StatusClaimed: {
			domain.StatusOpen: true, domain.StatusDelivered: true, domain.StatusAbandoned: true,
		},
		domain.StatusDelivered: {
			domain.StatusClaimed: true, domain.StatusCompleted: true, domain.StatusAbandoned: true,
		},
		domain.StatusCompleted: {},
		domain.StatusAbandoned: {},
	}

	for _, from := range statuses {
		for _, to := range statuses {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				// Retrospective is pre-filled so this matrix tests only the
				// status-graph edge, not the separate retrospective-required
				// rule (covered by the "abandon without retrospective" case
				// in TestValidateTransition above).
				b := domain.Bounty{Status: from, Retrospective: "非空结论,规避 ErrRetrospectiveRequired"}
				err := domain.ValidateTransition(b, to)
				if allowed[from][to] {
					require.NoError(t, err, "%s -> %s must be permitted", from, to)
					return
				}
				require.ErrorIs(t, err, domain.ErrInvalidTransition, "%s -> %s must be rejected", from, to)
			})
		}
	}
}

func TestCanClaim(t *testing.T) {
	eng := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleEngineer}}
	other := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleEngineer}}
	pm := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleSponsor}}

	t.Run("open public bounty is claimable by an engineer", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusOpen, Visibility: domain.VisibilityPublic}
		require.NoError(t, domain.CanClaim(eng, b))
	})

	t.Run("non engineer cannot claim", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusOpen, Visibility: domain.VisibilityPublic}
		require.ErrorIs(t, domain.CanClaim(pm, b), domain.ErrForbidden)
	})

	t.Run("claimed bounty is not claimable", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusClaimed, Visibility: domain.VisibilityPublic}
		require.ErrorIs(t, domain.CanClaim(eng, b), domain.ErrNotClaimable)
	})

	t.Run("directed bounty rejects everyone but the target", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusOpen, Visibility: domain.VisibilityDirected, DirectedTo: &eng.ID}
		require.NoError(t, domain.CanClaim(eng, b))
		require.ErrorIs(t, domain.CanClaim(other, b), domain.ErrNotDirectedToYou)
	})

	t.Run("directed bounty allows target without engineer role", func(t *testing.T) {
		b := domain.Bounty{Status: domain.StatusOpen, Visibility: domain.VisibilityDirected, DirectedTo: &pm.ID}
		require.NoError(t, domain.CanClaim(pm, b))
	})
}

func TestCanEditAndNominate(t *testing.T) {
	sponsor := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleSponsor}}
	claimer := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleEngineer}}
	steward := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleSteward}}
	stranger := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleEngineer}}

	b := domain.Bounty{SponsorID: sponsor.ID, ClaimedBy: &claimer.ID}

	require.True(t, domain.CanEdit(sponsor, b))
	require.True(t, domain.CanEdit(steward, b))
	require.False(t, domain.CanEdit(stranger, b))

	require.True(t, domain.CanNominate(claimer, b))
	require.True(t, domain.CanNominate(steward, b))
	require.False(t, domain.CanNominate(sponsor, b))

	t.Run("CanNominate with nil ClaimedBy", func(t *testing.T) {
		eng := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleEngineer}}
		stwd := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleSteward}}
		bUnclaimed := domain.Bounty{SponsorID: sponsor.ID, ClaimedBy: nil}

		require.False(t, domain.CanNominate(eng, bUnclaimed))
		require.True(t, domain.CanNominate(stwd, bUnclaimed))
	})
}

func TestBusinessLineWeightSum(t *testing.T) {
	sum := domain.BusinessLineWeightSum([]domain.BusinessLine{
		{Tag: "DSP", Weight: 0.7},
		{Tag: "ADX", Weight: 0.3},
	})
	require.InDelta(t, 1.0, sum, 1e-9)
}
