package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

// amend POSTs to /api/bounties/{id}/amend as actor.
func amend(t *testing.T, h http.Handler, id, actor string, body map[string]any) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/legacy/bounties/"+id+"/amend", actor, body)
}

// TestStewardCanAmendCompletedRetrospective pins A4a: the status graph is a
// hard gate with no edit endpoint, so once a bounty reaches a terminal
// status a typo'd retrospective can only ever be fixed through this
// steward-only correction channel.
func TestStewardCanAmendCompletedRetrospective(t *testing.T) {
	h := newTestServer(t)
	b := completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	rec := amend(t, h, b.ID.String(), stewardID, map[string]any{
		"retrospective": "补充结论:上线后 P99 从 80ms 降到 46ms",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var updated domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.Equal(t, "补充结论:上线后 P99 从 80ms 降到 46ms", updated.Retrospective)
	require.Equal(t, domain.StatusCompleted, updated.Status, "amend must not change status")
	require.Equal(t, b.SponsorID, updated.SponsorID, "amend must not change sponsor_id")
	require.Equal(t, b.ClaimedBy, updated.ClaimedBy, "amend must not change claimed_by")
}

// TestNonStewardCannotAmend covers both the sponsor and the claimer — neither
// holds the STEWARD role, and this is a correction channel, not an editor
// available to the parties on the bounty.
func TestNonStewardCannotAmend(t *testing.T) {
	h := newTestServer(t)
	b := completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	rec := amend(t, h, b.ID.String(), pmID, map[string]any{"retrospective": "产品 A 尝试修正"})
	require.Equal(t, http.StatusForbidden, rec.Code, "the sponsor is not a steward and must be refused")

	rec = amend(t, h, b.ID.String(), engCID, map[string]any{"retrospective": "claimer 尝试修正"})
	require.Equal(t, http.StatusForbidden, rec.Code, "the claimer is not a steward and must be refused")
}

// TestAmendOnlyTouchesRetrospectiveAndPersonDays guards against A4a's scope
// creeping into a general editor: amendRequest has no field for status,
// sponsor_id, claimed_by or business_lines, so a request smuggling those in
// the JSON body must have zero effect.
func TestAmendOnlyTouchesRetrospectiveAndPersonDays(t *testing.T) {
	h := newTestServer(t)
	b := completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	rec := amend(t, h, b.ID.String(), stewardID, map[string]any{
		"person_days": 9.5,
		"status":      "ABANDONED",
		"sponsor_id":  engCID,
		"claimed_by":  nil,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var updated domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.NotNil(t, updated.PersonDays)
	require.InDelta(t, 9.5, *updated.PersonDays, 1e-9)
	require.Equal(t, domain.StatusCompleted, updated.Status, "status field in the body must be ignored")
	require.Equal(t, b.SponsorID, updated.SponsorID, "sponsor_id field in the body must be ignored")
	require.Equal(t, b.ClaimedBy, updated.ClaimedBy, "claimed_by field in the body must be ignored")
}

// reset POSTs to /api/credits/{id}/reset as actor.
func resetCredit(t *testing.T, h http.Handler, id, actor string, body map[string]any) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/legacy/credits/"+id+"/reset", actor, body)
}

// declinedCredit nominates a credit on a fresh claimed bounty and has the
// nominee decline it, returning the declined credit.
func declinedCredit(t *testing.T, h http.Handler) domain.Credit {
	t.Helper()
	b := claimedBounty(t, h)
	c := nominateCredit(t, h, b, engDID, "SUPPORT", "")
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodPost, "/api/legacy/credits/"+c.ID.String()+"/respond", engDID,
			map[string]any{"status": "DECLINED"}).Code)
	c.Status = domain.CreditDeclined
	return c
}

// TestStewardCanResetDeclinedCredit pins A4b: a steward may move a
// mistakenly declined credit back to PENDING so its nominee can decide
// again.
func TestStewardCanResetDeclinedCredit(t *testing.T) {
	h := newTestServer(t)
	c := declinedCredit(t, h)

	rec := resetCredit(t, h, c.ID.String(), stewardID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var out domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, domain.CreditPending, out.Status)
	require.Nil(t, out.ConfirmedAt)
}

// TestCreditResetNeverConfirms is the specification's one hard constraint,
// exercised at the one endpoint that could plausibly break it: even if the
// caller sends a body naming CONFIRMED, reset has no status field to read it
// into, so the outcome must still be PENDING, never CONFIRMED.
func TestCreditResetNeverConfirms(t *testing.T) {
	h := newTestServer(t)
	c := declinedCredit(t, h)

	rec := resetCredit(t, h, c.ID.String(), stewardID, map[string]any{"status": "CONFIRMED"})
	require.Equal(t, http.StatusOK, rec.Code)

	var out domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, domain.CreditPending, out.Status,
		"reset must never produce CONFIRMED regardless of what the request body asks for")
}

// TestCreditResetRefusesNonDeclinedSource covers both PENDING and CONFIRMED
// as source states: only DECLINED may be reset.
func TestCreditResetRefusesNonDeclinedSource(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	pending := nominateCredit(t, h, b, engDID, "SUPPORT", "")
	rec := resetCredit(t, h, pending.ID.String(), stewardID, nil)
	require.Equal(t, http.StatusConflict, rec.Code, "resetting a PENDING credit must be refused")

	confirmed := nominateCredit(t, h, b, stewardID, "REVIEW", "https://git/mr/1#note-1")
	confirmCredit(t, h, confirmed, stewardID)
	rec = resetCredit(t, h, confirmed.ID.String(), stewardID, nil)
	require.Equal(t, http.StatusConflict, rec.Code, "resetting a CONFIRMED credit must be refused")
}

// TestNonStewardCannotResetCredit covers both the nominee and the claimer who
// nominated them — neither holds STEWARD.
func TestNonStewardCannotResetCredit(t *testing.T) {
	h := newTestServer(t)
	c := declinedCredit(t, h)

	require.Equal(t, http.StatusForbidden, resetCredit(t, h, c.ID.String(), engDID, nil).Code,
		"the nominee is not a steward and must be refused")
	require.Equal(t, http.StatusForbidden, resetCredit(t, h, c.ID.String(), engCID, nil).Code,
		"the nominator is not a steward and must be refused")
}
