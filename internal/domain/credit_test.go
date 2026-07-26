package domain_test

import (
	"testing"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateNomination(t *testing.T) {
	cases := []struct {
		name     string
		role     domain.CreditRole
		evidence string
		wantErr  error
	}{
		{"define needs no evidence", domain.CreditRoleDefine, "", nil},
		{"lead needs no evidence", domain.CreditRoleLead, "", nil},
		{"co_deliver needs no evidence", domain.CreditRoleCoDeliver, "", nil},
		{"review without evidence is rejected", domain.CreditRoleReview, "", domain.ErrEvidenceRequired},
		{"review with evidence is accepted", domain.CreditRoleReview, "https://git/mr/42#note-7", nil},
		{"support needs no evidence", domain.CreditRoleSupport, "", nil},
		{"baseline needs no evidence", domain.CreditRoleBaseline, "", nil},
		{"unknown role is rejected", domain.CreditRole("PRAISE"), "", domain.ErrInvalidCreditRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateNomination(domain.Credit{Role: tc.role, Evidence: tc.evidence})
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestCanRespond(t *testing.T) {
	nominee := domain.User{ID: uuid.New()}
	other := domain.User{ID: uuid.New()}
	steward := domain.User{ID: uuid.New(), Roles: []domain.UserRole{domain.UserRoleSteward}}

	pending := domain.Credit{UserID: nominee.ID, Status: domain.CreditPending}

	require.NoError(t, domain.CanRespond(nominee, pending))

	// Confirming a credit is a personal act. Even the steward cannot confirm on
	// someone's behalf — that is exactly the forgery this rule prevents.
	require.ErrorIs(t, domain.CanRespond(other, pending), domain.ErrNotYourCredit)
	require.ErrorIs(t, domain.CanRespond(steward, pending), domain.ErrNotYourCredit)

	confirmed := domain.Credit{UserID: nominee.ID, Status: domain.CreditConfirmed}
	require.ErrorIs(t, domain.CanRespond(nominee, confirmed), domain.ErrCreditNotPending)

	declined := domain.Credit{UserID: nominee.ID, Status: domain.CreditDeclined}
	require.ErrorIs(t, domain.CanRespond(nominee, declined), domain.ErrCreditNotPending)
}
