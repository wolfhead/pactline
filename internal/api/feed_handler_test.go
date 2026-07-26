package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/api"
	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func completeWithCredit(t *testing.T, h http.Handler, nomineeID string, role string, evidence string) domain.Bounty {
	t.Helper()
	b := claimedBounty(t, h)

	body := map[string]any{"user_id": nomineeID, "role": role}
	if evidence != "" {
		body["evidence"] = evidence
	}
	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID, body)
	require.Equal(t, http.StatusCreated, rec.Code)
	var c domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/credits/"+c.ID.String()+"/respond", nomineeID,
			map[string]any{"status": "CONFIRMED"}).Code)

	transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 2})
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)
	return b
}

func TestWorkFeedIncludesCompletedAndAbandoned(t *testing.T) {
	h := newTestServer(t)
	completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	failed := claimedBounty(t, h)
	require.Equal(t, http.StatusOK, transition(t, h, failed.ID.String(), engCID, map[string]any{
		"to": "ABANDONED", "retrospective": "SSP 接口未就绪,结论:先补 mock 层",
	}).Code)

	rec := do(t, h, http.MethodGet, "/api/works", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var feed []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &feed))
	require.Len(t, feed, 2)

	var sawAbandoned bool
	for _, w := range feed {
		if w.Bounty.Status == domain.StatusAbandoned {
			sawAbandoned = true
			require.NotEmpty(t, w.Bounty.Retrospective, "abandoned work must carry its conclusion")
		}
	}
	require.True(t, sawAbandoned, "abandoned work must appear in the feed")
}

func TestWorkFeedOmitsUnconfirmedCredits(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	// Nominated but never confirmed.
	require.Equal(t, http.StatusCreated,
		do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID,
			map[string]any{"user_id": engDID, "role": "SUPPORT"}).Code)

	transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED"})
	transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"})

	rec := do(t, h, http.MethodGet, "/api/works", pmID, nil)
	var feed []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &feed))
	require.Len(t, feed, 1)
	require.Empty(t, feed[0].Credits, "unconfirmed credits must not surface in the feed")
}

func TestPortfolioListsOnlyConfirmedCredits(t *testing.T) {
	h := newTestServer(t)
	completeWithCredit(t, h, engDID, "REVIEW", "https://git/mr/42#note-7")

	rec := do(t, h, http.MethodGet, "/api/users/"+engDID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var works []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &works))
	require.Len(t, works, 1)
	require.Len(t, works[0].Credits, 1)
	require.Equal(t, domain.CreditRoleReview, works[0].Credits[0].Credit.Role)
	require.Equal(t, "研发 D", works[0].Credits[0].UserName)
}

// TestPortfolioOfUnknownUserIs404 distinguishes "no works yet" (a real user
// with zero credits, covered by TestPortfolioOfUncreditedUserIsEmpty below)
// from "the system has never heard of this person" — the two must not read
// the same over the wire.
func TestPortfolioOfUnknownUserIs404(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/users/11111111-1111-1111-1111-111111111111/portfolio", pmID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPortfolioOfUncreditedUserIsEmpty(t *testing.T) {
	h := newTestServer(t)
	completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	rec := do(t, h, http.MethodGet, "/api/users/"+stewardID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var works []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &works))
	require.Empty(t, works)
}

// nominateCredit nominates role for nomineeID on b, acting as the bounty's
// claimer (engCID), and returns the created credit.
func nominateCredit(t *testing.T, h http.Handler, b domain.Bounty, nomineeID, role, evidence string) domain.Credit {
	t.Helper()
	body := map[string]any{"user_id": nomineeID, "role": role}
	if evidence != "" {
		body["evidence"] = evidence
	}
	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID, body)
	require.Equal(t, http.StatusCreated, rec.Code)
	var c domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))
	return c
}

// confirmCredit confirms c as its nominee.
func confirmCredit(t *testing.T, h http.Handler, c domain.Credit, nomineeID string) {
	t.Helper()
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/credits/"+c.ID.String()+"/respond", nomineeID,
			map[string]any{"status": "CONFIRMED"}).Code)
}

// findWork locates the WorkView for bountyID within a /api/works response.
func findWork(feed []api.WorkView, bountyID uuid.UUID) *api.WorkView {
	for i := range feed {
		if feed[i].Bounty.ID == bountyID {
			return &feed[i]
		}
	}
	return nil
}

// TestWorkFeedOrdersCreditsByRole proves creditRoleOrder actually governs the
// response: several confirmed credits in different roles are nominated in an
// order that does not match the display order (SUPPORT, then DEFINE, then
// REVIEW), and the feed must still return them DEFINE, REVIEW, SUPPORT.
func TestWorkFeedOrdersCreditsByRole(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	const techLeaderB = "00000000-0000-0000-0000-000000000002"

	support := nominateCredit(t, h, b, engDID, "SUPPORT", "")
	define := nominateCredit(t, h, b, stewardID, "DEFINE", "")
	review := nominateCredit(t, h, b, techLeaderB, "REVIEW", "https://git/mr/99#note-1")

	confirmCredit(t, h, support, engDID)
	confirmCredit(t, h, define, stewardID)
	confirmCredit(t, h, review, techLeaderB)

	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 1}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)

	rec := do(t, h, http.MethodGet, "/api/works", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var feed []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &feed))

	work := findWork(feed, b.ID)
	require.NotNil(t, work, "completed bounty must appear in the feed")
	require.Len(t, work.Credits, 3)
	require.Equal(t, domain.CreditRoleDefine, work.Credits[0].Credit.Role)
	require.Equal(t, domain.CreditRoleReview, work.Credits[1].Credit.Role)
	require.Equal(t, domain.CreditRoleSupport, work.Credits[2].Credit.Role)
}

// TestWorkFeedKeepsNominationOrderForTiedRoles covers A2: two different users
// can legitimately hold the same role on one bounty (the unique constraint is
// (bounty_id, user_id, role), not (bounty_id, role)), and their relative order
// in the response must be deterministic — nomination order, since
// ListByBounty returns rows ordered by created_at and the sort must be stable.
func TestWorkFeedKeepsNominationOrderForTiedRoles(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	const engEID = "00000000-0000-0000-0000-000000000005"

	first := nominateCredit(t, h, b, engDID, "SUPPORT", "")
	second := nominateCredit(t, h, b, engEID, "SUPPORT", "")

	confirmCredit(t, h, first, engDID)
	confirmCredit(t, h, second, engEID)

	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 1}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)

	rec := do(t, h, http.MethodGet, "/api/works", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var feed []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &feed))

	work := findWork(feed, b.ID)
	require.NotNil(t, work, "completed bounty must appear in the feed")
	require.Len(t, work.Credits, 2)
	require.Equal(t, domain.CreditRoleSupport, work.Credits[0].Credit.Role)
	require.Equal(t, domain.CreditRoleSupport, work.Credits[1].Credit.Role)
	require.Equal(t, engDID, work.Credits[0].Credit.UserID.String(), "tied roles must keep nomination order")
	require.Equal(t, engEID, work.Credits[1].Credit.UserID.String())
}
