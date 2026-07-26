package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNominateIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	c := domain.Credit{BountyID: b.ID, UserID: userEngC, Role: domain.CreditRoleLead, NominatedBy: &userPM}
	first, err := cs.Nominate(ctx, c)
	require.NoError(t, err)
	second, err := cs.Nominate(ctx, c)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	all, err := cs.ListByBounty(ctx, b.ID)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, domain.CreditPending, all[0].Status)
}

func TestRespondConfirmsAndStamps(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	c, err := cs.Nominate(ctx, domain.Credit{
		BountyID: b.ID, UserID: userEngC, Role: domain.CreditRoleSupport, NominatedBy: &userPM,
	})
	require.NoError(t, err)

	confirmed, err := cs.Respond(ctx, c.ID, domain.CreditConfirmed)
	require.NoError(t, err)
	require.Equal(t, domain.CreditConfirmed, confirmed.Status)
	require.NotNil(t, confirmed.ConfirmedAt)
}

// Regression guard for the mechanism's only hard constraint: unconfirmed
// credits must not appear in any tally. See spec section 6.2.
func TestUnconfirmedCreditsAreExcludedFromPortfolio(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	pendingBounty, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, pendingBounty.ID)
	_, err = cs.Nominate(ctx, domain.Credit{
		BountyID: pendingBounty.ID, UserID: userEngD, Role: domain.CreditRoleSupport, NominatedBy: &userPM,
	})
	require.NoError(t, err)

	confirmedBounty, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, confirmedBounty.ID)
	c, err := cs.Nominate(ctx, domain.Credit{
		BountyID: confirmedBounty.ID, UserID: userEngD, Role: domain.CreditRoleLead, NominatedBy: &userPM,
	})
	require.NoError(t, err)
	_, err = cs.Respond(ctx, c.ID, domain.CreditConfirmed)
	require.NoError(t, err)

	declinedBounty, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, declinedBounty.ID)
	d, err := cs.Nominate(ctx, domain.Credit{
		BountyID: declinedBounty.ID, UserID: userEngD, Role: domain.CreditRoleReview,
		Evidence: "https://git/mr/1", NominatedBy: &userPM,
	})
	require.NoError(t, err)
	_, err = cs.Respond(ctx, d.ID, domain.CreditDeclined)
	require.NoError(t, err)

	byBounty, err := cs.ListConfirmedBountyIDsForUser(ctx, userEngD)
	require.NoError(t, err)
	require.Len(t, byBounty, 1)
	require.Contains(t, byBounty, confirmedBounty.ID)
	require.NotContains(t, byBounty, pendingBounty.ID)
	require.NotContains(t, byBounty, declinedBounty.ID)
}

func TestInheritDefineCreditsFromParentLead(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	plan := newBounty(userPM)
	plan.Type = domain.BountyTypePlan
	plan.Status = domain.StatusCompleted
	plan.ClaimedBy = &userEngC
	planned, err := bs.Create(ctx, plan)
	require.NoError(t, err)
	cleanupBounties(t, db, planned.ID)

	lead, err := cs.Nominate(ctx, domain.Credit{
		BountyID: planned.ID, UserID: userEngC, Role: domain.CreditRoleLead, NominatedBy: &userPM,
	})
	require.NoError(t, err)
	_, err = cs.Respond(ctx, lead.ID, domain.CreditConfirmed)
	require.NoError(t, err)

	child := newBounty(userPM)
	child.ParentID = &planned.ID
	delivery, err := bs.Create(ctx, child)
	require.NoError(t, err)
	cleanupBounties(t, db, delivery.ID)

	n, err := cs.InheritDefineCredits(ctx, delivery)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	credits, err := cs.ListByBounty(ctx, delivery.ID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, domain.CreditRoleDefine, credits[0].Role)
	require.Equal(t, userEngC, credits[0].UserID)
	require.Nil(t, credits[0].NominatedBy, "system-generated credit has no nominator")
	require.Equal(t, domain.CreditPending, credits[0].Status, "inherited credit still needs confirmation")
}

func TestInheritDefineCreditsFallsBackToClaimer(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	plan := newBounty(userPM)
	plan.Type = domain.BountyTypePlan
	plan.ClaimedBy = &userEngD
	planned, err := bs.Create(ctx, plan)
	require.NoError(t, err)
	cleanupBounties(t, db, planned.ID)

	child := newBounty(userPM)
	child.ParentID = &planned.ID
	delivery, err := bs.Create(ctx, child)
	require.NoError(t, err)
	cleanupBounties(t, db, delivery.ID)

	n, err := cs.InheritDefineCredits(ctx, delivery)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	credits, err := cs.ListByBounty(ctx, delivery.ID)
	require.NoError(t, err)
	require.Equal(t, userEngD, credits[0].UserID)
}

func TestInheritDefineCreditsNoParentIsNoop(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	n, err := cs.InheritDefineCredits(ctx, b)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, mustCredits(t, cs, b.ID))
}

func mustCredits(t *testing.T, cs *store.CreditStore, id uuid.UUID) []domain.Credit {
	t.Helper()
	out, err := cs.ListByBounty(context.Background(), id)
	require.NoError(t, err)
	return out
}

// TestInheritDefineCreditsIgnoresUnconfirmedLead pins the CONFIRMED filter in
// confirmedLeadHolders: a PENDING lead must not be treated as an author, and
// inheritance must fall back to the parent's claimed_by instead. A
// regression that dropped `status='CONFIRMED'` from that query's WHERE
// clause would pass every other existing test, since they only ever exercise
// the case where the lead is already confirmed.
func TestInheritDefineCreditsIgnoresUnconfirmedLead(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	plan := newBounty(userPM)
	plan.Type = domain.BountyTypePlan
	plan.ClaimedBy = &userEngD
	planned, err := bs.Create(ctx, plan)
	require.NoError(t, err)
	cleanupBounties(t, db, planned.ID)

	// Nominate a LEAD credit for a different user, but never confirm it.
	_, err = cs.Nominate(ctx, domain.Credit{
		BountyID: planned.ID, UserID: userEngC, Role: domain.CreditRoleLead, NominatedBy: &userPM,
	})
	require.NoError(t, err)

	child := newBounty(userPM)
	child.ParentID = &planned.ID
	delivery, err := bs.Create(ctx, child)
	require.NoError(t, err)
	cleanupBounties(t, db, delivery.ID)

	n, err := cs.InheritDefineCredits(ctx, delivery)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	credits, err := cs.ListByBounty(ctx, delivery.ID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, domain.CreditRoleDefine, credits[0].Role)
	require.Equal(t, userEngD, credits[0].UserID, "unconfirmed LEAD holder must be ignored; fallback to claimed_by")
}

// TestInheritDefineCreditsNeitherLeadNorClaimerIsNoop covers the branch where
// the parent plan has no confirmed LEAD credit and no claimer at all: there
// is nobody to credit, so inheritance must return (0, nil) and leave the
// child with no credits, rather than nominating a zero-value user.
func TestInheritDefineCreditsNeitherLeadNorClaimerIsNoop(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	plan := newBounty(userPM)
	plan.Type = domain.BountyTypePlan
	plan.ClaimedBy = nil
	planned, err := bs.Create(ctx, plan)
	require.NoError(t, err)
	cleanupBounties(t, db, planned.ID)

	child := newBounty(userPM)
	child.ParentID = &planned.ID
	delivery, err := bs.Create(ctx, child)
	require.NoError(t, err)
	cleanupBounties(t, db, delivery.ID)

	n, err := cs.InheritDefineCredits(ctx, delivery)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, mustCredits(t, cs, delivery.ID))
}

// TestRespondDeclineLeavesConfirmedAtNil is the DECLINE counterpart to
// TestRespondConfirmsAndStamps: only the confirm path stamps confirmed_at.
func TestRespondDeclineLeavesConfirmedAtNil(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	c, err := cs.Nominate(ctx, domain.Credit{
		BountyID: b.ID, UserID: userEngC, Role: domain.CreditRoleSupport, NominatedBy: &userPM,
	})
	require.NoError(t, err)

	declined, err := cs.Respond(ctx, c.ID, domain.CreditDeclined)
	require.NoError(t, err)
	require.Equal(t, domain.CreditDeclined, declined.Status)
	require.Nil(t, declined.ConfirmedAt)
}

// TestNominateConflictKeepsOriginalEvidence pins the idempotent-wins behavior
// from A1: re-nominating the same (bounty, user, role) with different data
// must not overwrite what was already stored, and the discard must be
// something a future change to upsert semantics has to break this test to
// introduce.
func TestNominateConflictKeepsOriginalEvidence(t *testing.T) {
	db := newTestDB(t)
	bs, cs := store.NewBountyStore(db), store.NewCreditStore(db)
	ctx := context.Background()

	b, err := bs.Create(ctx, newBounty(userPM))
	require.NoError(t, err)
	cleanupBounties(t, db, b.ID)

	first, err := cs.Nominate(ctx, domain.Credit{
		BountyID: b.ID, UserID: userEngC, Role: domain.CreditRoleReview,
		Evidence: "https://git/mr/1", NominatedBy: &userPM,
	})
	require.NoError(t, err)

	second, err := cs.Nominate(ctx, domain.Credit{
		BountyID: b.ID, UserID: userEngC, Role: domain.CreditRoleReview,
		Evidence: "https://git/mr/2", NominatedBy: &userPM,
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "https://git/mr/1", second.Evidence, "conflict must not overwrite the originally stored evidence")

	all, err := cs.ListByBounty(ctx, b.ID)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "https://git/mr/1", all[0].Evidence)
}
