package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/domain"

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

	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
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

	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", pmID, map[string]any{
		"user_id": engDID, "role": "SUPPORT",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestReviewCreditRequiresEvidence(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	path := "/api/bounties/" + b.ID.String() + "/credits"

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
	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
		"user_id": engDID, "role": "PRAISE",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOnlyNomineeMayRespond(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	rec := do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID, map[string]any{
		"user_id": engDID, "role": "SUPPORT",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var c domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))

	respond := "/api/credits/" + c.ID.String() + "/respond"

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

func TestPendingListShowsOnlyMine(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)
	do(t, h, http.MethodPost, "/api/bounties/"+b.ID.String()+"/credits", engCID,
		map[string]any{"user_id": engDID, "role": "SUPPORT"})

	rec := do(t, h, http.MethodGet, "/api/credits/pending", engDID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var mine []domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &mine))
	require.NotEmpty(t, mine)
	for _, c := range mine {
		require.Equal(t, engDID, c.UserID.String())
		require.Equal(t, domain.CreditPending, c.Status)
	}
}

func TestDeliveryBountyInheritsDefineCredit(t *testing.T) {
	h := newTestServer(t)

	// A plan bounty carried to completion by 研发 C.
	plan := createDraft(t, h)
	transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "OPEN"})
	transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "CLAIMED"})
	transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "DELIVERED"})
	transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "COMPLETED"})

	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type": "DELIVERY", "title": "按方案实现降延迟", "parent_id": plan.ID.String(),
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var child domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &child))
	cleanupBounty(t, child.ID)

	list := do(t, h, http.MethodGet, "/api/bounties/"+child.ID.String()+"/credits", pmID, nil)
	require.Equal(t, http.StatusOK, list.Code)
	var credits []domain.Credit
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &credits))
	require.Len(t, credits, 1)
	require.Equal(t, domain.CreditRoleDefine, credits[0].Role)
	require.Equal(t, engCID, credits[0].UserID.String())
	require.Nil(t, credits[0].NominatedBy)
	require.Equal(t, domain.CreditPending, credits[0].Status)
}
