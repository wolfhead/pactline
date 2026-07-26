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

// TestPortfolioListsOnlyConfirmedCredits pins the CONFIRMED filter that
// decides which works enter engDID's portfolio. Beyond the one confirmed
// credit, engDID also holds a PENDING credit and a DECLINED credit on two
// other terminal (COMPLETED) works. Both extra works must be absent: if
// ListConfirmedBountyIDsForUser's status='CONFIRMED' filter were dropped,
// either decoy bounty would leak into the response as an extra WorkView.
func TestPortfolioListsOnlyConfirmedCredits(t *testing.T) {
	h := newTestServer(t)
	confirmed := completeWithCredit(t, h, engDID, "REVIEW", "https://git/mr/42#note-7")

	pendingWork := claimedBounty(t, h)
	nominateCredit(t, h, pendingWork, engDID, "SUPPORT", "")
	require.Equal(t, http.StatusOK,
		transition(t, h, pendingWork.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 1}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, pendingWork.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)

	declinedWork := claimedBounty(t, h)
	declinedCredit := nominateCredit(t, h, declinedWork, engDID, "SUPPORT", "")
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/credits/"+declinedCredit.ID.String()+"/respond", engDID,
			map[string]any{"status": "DECLINED"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, declinedWork.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 1}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, declinedWork.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)

	rec := do(t, h, http.MethodGet, "/api/users/"+engDID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var works []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &works))
	require.Len(t, works, 1, "PENDING and DECLINED credits on other works must not surface them")
	require.Equal(t, confirmed.ID, works[0].Bounty.ID)
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
// response, using roles chosen so that display order, alphabetical order and
// nomination order all disagree with one another:
//
//	display order (by part):  DEFINE, LEAD, CO_DELIVER, SUPPORT
//	alphabetical order:       CO_DELIVER, DEFINE, LEAD, SUPPORT
//	nomination order:         SUPPORT, CO_DELIVER, LEAD, DEFINE
//
// CO_DELIVER is the key discriminator: it sorts before DEFINE alphabetically
// but after it by part. A prior version of this test used only
// DEFINE/REVIEW/SUPPORT, whose alphabetical order happens to equal display
// order, so swapping creditRoleOrder for a plain string compare (or sorting
// by creation order) would have still passed.
func TestWorkFeedOrdersCreditsByRole(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	const techLeaderB = "00000000-0000-0000-0000-000000000002"
	const engEID = "00000000-0000-0000-0000-000000000005"

	support := nominateCredit(t, h, b, stewardID, "SUPPORT", "")
	coDeliver := nominateCredit(t, h, b, engDID, "CO_DELIVER", "")
	lead := nominateCredit(t, h, b, engEID, "LEAD", "")
	define := nominateCredit(t, h, b, techLeaderB, "DEFINE", "")

	confirmCredit(t, h, support, stewardID)
	confirmCredit(t, h, coDeliver, engDID)
	confirmCredit(t, h, lead, engEID)
	confirmCredit(t, h, define, techLeaderB)

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
	require.Len(t, work.Credits, 4)
	require.Equal(t,
		[]domain.CreditRole{domain.CreditRoleDefine, domain.CreditRoleLead, domain.CreditRoleCoDeliver, domain.CreditRoleSupport},
		[]domain.CreditRole{work.Credits[0].Credit.Role, work.Credits[1].Credit.Role, work.Credits[2].Credit.Role, work.Credits[3].Credit.Role},
		"credits are ordered by part, not alphabetically and not by nomination order")
}

// TestWorkFeedKeepsNominationOrderForTiedRoles covers A2: several different
// users can legitimately hold the same role on one bounty (the unique
// constraint is (bounty_id, user_id, role), not (bounty_id, role)), and their
// relative order in the response must be deterministic — nomination order,
// since ListByBounty returns rows ordered by created_at and the sort must be
// stable.
//
// The six seeded users are tied on SUPPORT, nominated in an order that
// disagrees with ascending user id at every position:
//
//	nomination order:    engDID(04), stewardID(06), pmID(01), techLeaderB(02), engEID(05), engCID(03)
//	ascending user id:   pmID(01), techLeaderB(02), engCID(03), engDID(04), engEID(05), stewardID(06)
//
// That alone is not a sufficient fixture, though: Go's sort package falls
// back to a plain insertion sort — which only swaps on strict-less and so
// never reorders equal elements — for any slice of at most 12 elements (see
// sort/zsortfunc.go's maxInsertion), and with only six seeded users the tied
// SUPPORT group alone can never exceed six. So every user is additionally
// given every OTHER role on this same bounty too (5 roles x 6 users = 30 more
// credits), pushing the bounty's total confirmed-credit count on the feed to
// 36 — well past that threshold, so decorate's sort actually exercises its
// general-case path instead of the always-stable small-slice fallback.
func TestWorkFeedKeepsNominationOrderForTiedRoles(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	const techLeaderB = "00000000-0000-0000-0000-000000000002"
	const engEID = "00000000-0000-0000-0000-000000000005"

	tiedOrder := []string{engDID, stewardID, pmID, techLeaderB, engEID, engCID}
	for _, u := range tiedOrder {
		c := nominateCredit(t, h, b, u, "SUPPORT", "")
		confirmCredit(t, h, c, u)
	}

	allUsers := []string{pmID, techLeaderB, engCID, engDID, engEID, stewardID}
	otherRoles := []string{"DEFINE", "LEAD", "CO_DELIVER", "REVIEW", "BASELINE"}
	for _, u := range allUsers {
		for _, role := range otherRoles {
			evidence := ""
			if role == "REVIEW" {
				evidence = "https://git/mr/1#note-1"
			}
			c := nominateCredit(t, h, b, u, role, evidence)
			confirmCredit(t, h, c, u)
		}
	}

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
	require.Len(t, work.Credits, len(tiedOrder)+len(allUsers)*len(otherRoles))

	var gotSupportOrder []string
	for _, cv := range work.Credits {
		if cv.Credit.Role == domain.CreditRoleSupport {
			gotSupportOrder = append(gotSupportOrder, cv.Credit.UserID.String())
		}
	}
	require.Equal(t, tiedOrder, gotSupportOrder, "tied roles must keep nomination order")
}
