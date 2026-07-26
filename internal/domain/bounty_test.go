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
}

func TestBusinessLineWeightSum(t *testing.T) {
	sum := domain.BusinessLineWeightSum([]domain.BusinessLine{
		{Tag: "DSP", Weight: 0.7},
		{Tag: "ADX", Weight: 0.3},
	})
	require.InDelta(t, 1.0, sum, 1e-9)
}
