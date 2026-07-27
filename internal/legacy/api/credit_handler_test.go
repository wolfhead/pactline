package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

// claimedBounty drives a fresh bounty to CLAIMED by 研发 C.
func claimedBounty(t *testing.T, h http.Handler) domain.Bounty {
	t.Helper()
	b := createDraft(t, h)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	rec := transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"})
	require.Equal(t, http.StatusOK, rec.Code)
	var claimed domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	return claimed
}

func TestDelivererNominatesCredit(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	rec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
		"user_id": engDID, "role": "SUPPORT",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var c domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))
	require.Equal(t, domain.CreditPending, c.Status)
	require.NotNil(t, c.NominatedBy)
	require.Equal(t, engCID, c.NominatedBy.String())
}

func TestSponsorCannotNominate(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	rec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+b.ID.String()+"/credits", pmID, map[string]any{
		"user_id": engDID, "role": "SUPPORT",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestReviewCreditRequiresEvidence(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	path := "/api/legacy/bounties/" + b.ID.String() + "/credits"

	rec := do(t, h, http.MethodPost, path, engCID, map[string]any{"user_id": engDID, "role": "REVIEW"})
	require.Equal(t, http.StatusConflict, rec.Code)

	rec = do(t, h, http.MethodPost, path, engCID, map[string]any{
		"user_id": engDID, "role": "REVIEW", "evidence": "https://git/mr/42#note-7",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestUnknownRoleIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	rec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
		"user_id": engDID, "role": "PRAISE",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOnlyNomineeMayRespond(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	rec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
		"user_id": engDID, "role": "SUPPORT",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var c domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))

	respond := "/api/legacy/credits/" + c.ID.String() + "/respond"

	// Not even the steward may confirm on the nominee's behalf.
	require.Equal(t, http.StatusForbidden,
		do(t, h, http.MethodPost, respond, stewardID, map[string]any{"status": "CONFIRMED"}).Code)
	require.Equal(t, http.StatusForbidden,
		do(t, h, http.MethodPost, respond, engCID, map[string]any{"status": "CONFIRMED"}).Code)

	ok := do(t, h, http.MethodPost, respond, engDID, map[string]any{"status": "CONFIRMED"})
	require.Equal(t, http.StatusOK, ok.Code)

	// Responding twice is a conflict.
	require.Equal(t, http.StatusConflict,
		do(t, h, http.MethodPost, respond, engDID, map[string]any{"status": "DECLINED"}).Code)
}

// TestPendingListShowsOnlyMine pins both filters ListPendingForUser applies:
// the target credit (engDID, PENDING) sits alongside a PENDING credit
// belonging to someone else (which only the user_id filter excludes) and a
// non-pending credit of engDID's own on a different bounty (which only the
// status filter excludes). Dropping either filter would leak one of these
// two decoys into the response.
func TestPendingListShowsOnlyMine(t *testing.T) {
	h := newTestServer(t)

	const engEID = "00000000-0000-0000-0000-000000000005"

	target := claimedBounty(t, h)
	targetRec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+target.ID.String()+"/credits", engCID,
		map[string]any{"user_id": engDID, "role": "SUPPORT"})
	require.Equal(t, http.StatusCreated, targetRec.Code)
	var targetCredit domain.Credit
	require.NoError(t, json.Unmarshal(targetRec.Body.Bytes(), &targetCredit))

	// Someone else's PENDING credit: only the user_id filter excludes this.
	otherBounty := claimedBounty(t, h)
	require.Equal(t, http.StatusCreated,
		do(t, h, http.MethodPost, "/api/legacy/bounties/"+otherBounty.ID.String()+"/credits", engCID,
			map[string]any{"user_id": engEID, "role": "SUPPORT"}).Code)

	// engDID's own credit, but already CONFIRMED: only the status filter
	// excludes this.
	confirmedBounty := claimedBounty(t, h)
	confirmedRec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+confirmedBounty.ID.String()+"/credits", engCID,
		map[string]any{"user_id": engDID, "role": "CO_DELIVER"})
	require.Equal(t, http.StatusCreated, confirmedRec.Code)
	var confirmedCredit domain.Credit
	require.NoError(t, json.Unmarshal(confirmedRec.Body.Bytes(), &confirmedCredit))
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/legacy/credits/"+confirmedCredit.ID.String()+"/respond", engDID,
			map[string]any{"status": "CONFIRMED"}).Code)

	rec := do(t, h, http.MethodGet, "/api/legacy/credits/pending", engDID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var mine []domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &mine))
	require.Len(t, mine, 1, "only engDID's own PENDING credit must be returned")
	require.Equal(t, targetCredit.ID, mine[0].ID)
	require.Equal(t, engDID, mine[0].UserID.String())
	require.Equal(t, domain.CreditPending, mine[0].Status)
}

func TestDeliveryBountyInheritsDefineCredit(t *testing.T) {
	h := newTestServer(t)

	// A plan bounty carried to completion by 研发 C.
	plan := createDraft(t, h)
	transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "OPEN"})
	transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "CLAIMED"})
	transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "DELIVERED"})
	transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "COMPLETED"})

	rec := do(t, h, http.MethodPost, "/api/legacy/bounties", pmID, map[string]any{
		"type": "DELIVERY", "title": "按方案实现降延迟", "parent_id": plan.ID.String(),
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var child domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &child))
	cleanupBounty(t, child.ID)

	list := do(t, h, http.MethodGet, "/api/legacy/bounties/"+child.ID.String()+"/credits", pmID, nil)
	require.Equal(t, http.StatusOK, list.Code)
	var credits []domain.Credit
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &credits))
	require.Len(t, credits, 1)
	require.Equal(t, domain.CreditRoleDefine, credits[0].Role)
	require.Equal(t, engCID, credits[0].UserID.String())
	require.Nil(t, credits[0].NominatedBy)
	require.Equal(t, domain.CreditPending, credits[0].Status)
}

// TestRespondWithEmptyBodyIsBadRequest pins that an empty request body is
// rejected by decodeBody (JSON decode of zero bytes fails with EOF) before
// respond ever reaches the credit or the nominee check.
func TestRespondWithEmptyBodyIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	c := nominateCredit(t, h, b, engDID, "SUPPORT", "")

	rec := do(t, h, http.MethodPost, "/api/legacy/credits/"+c.ID.String()+"/respond", engDID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestRespondWithMissingStatusIsBadRequest pins that a body with no "status"
// field decodes to the zero value (""), which the handler's
// CONFIRMED-or-DECLINED check must reject rather than silently no-op or crash.
func TestRespondWithMissingStatusIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	c := nominateCredit(t, h, b, engDID, "SUPPORT", "")

	rec := do(t, h, http.MethodPost, "/api/legacy/credits/"+c.ID.String()+"/respond", engDID, map[string]any{})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestRespondWithPendingStatusIsBadRequest pins a real bypass shape against
// the mechanism's one hard constraint (spec §6.2): a caller cannot "confirm"
// a credit by sending its already-current status back. status: PENDING must
// be rejected exactly like any other value outside {CONFIRMED, DECLINED}.
func TestRespondWithPendingStatusIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	c := nominateCredit(t, h, b, engDID, "SUPPORT", "")

	rec := do(t, h, http.MethodPost, "/api/legacy/credits/"+c.ID.String()+"/respond", engDID,
		map[string]any{"status": "PENDING"})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// The credit must still be normally confirmable afterward — the rejected
	// request must not have corrupted its state.
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/legacy/credits/"+c.ID.String()+"/respond", engDID,
			map[string]any{"status": "CONFIRMED"}).Code)
}
