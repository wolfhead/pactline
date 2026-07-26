package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/domain"

	"github.com/stretchr/testify/require"
)

const (
	stewardID = "00000000-0000-0000-0000-000000000006"
	engDID    = "00000000-0000-0000-0000-000000000004"
)

func createDraft(t *testing.T, h http.Handler) domain.Bounty {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type":  "DELIVERY",
		"title": "竞价链路降延迟",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	// The brief's createDraft did not register cleanup, which left DRAFT
	// bounties behind and broke internal/store tests that assert exact
	// counts against the shared database (see cleanupBounty's doc comment
	// in bounty_handler_test.go). Every transition test funnels through
	// this helper, so cleaning up here is sufficient for the whole file.
	cleanupBounty(t, b.ID)
	return b
}

func transition(t *testing.T, h http.Handler, id, actor string, body map[string]any) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/bounties/"+id+"/transition", actor, body)
}

func TestFullHappyPathTransitions(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)

	rec := transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"})
	require.Equal(t, http.StatusOK, rec.Code)
	var claimed domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	require.NotNil(t, claimed.ClaimedBy)
	require.Equal(t, engCID, claimed.ClaimedBy.String())
	require.NotNil(t, claimed.ClaimedAt)

	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 3.5}).Code)

	rec = transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"})
	require.Equal(t, http.StatusOK, rec.Code)
	var done domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &done))
	require.Equal(t, domain.StatusCompleted, done.Status)
	require.NotNil(t, done.CompletedAt)
	require.NotNil(t, done.PersonDays)
	require.InDelta(t, 3.5, *done.PersonDays, 1e-9)
}

func TestIllegalTransitionIsConflict(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	rec := transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"})
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestAbandonRequiresRetrospective(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := transition(t, h, b.ID.String(), pmID, map[string]any{"to": "ABANDONED"})
	require.Equal(t, http.StatusConflict, rec.Code)

	rec = transition(t, h, b.ID.String(), pmID, map[string]any{
		"to":            "ABANDONED",
		"retrospective": "上游 SSP 接口未按期提供,结论:先做 mock 层再重开",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var abandoned domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &abandoned))
	require.Equal(t, domain.StatusAbandoned, abandoned.Status)
	require.NotNil(t, abandoned.CompletedAt)
}

func TestNonEngineerCannotClaim(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)

	rec := transition(t, h, b.ID.String(), pmID, map[string]any{"to": "CLAIMED"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestUnclaimClearsClaimFields(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"})
	transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"})

	rec := transition(t, h, b.ID.String(), engCID, map[string]any{"to": "OPEN"})
	require.Equal(t, http.StatusOK, rec.Code)
	var back domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &back))
	require.Nil(t, back.ClaimedBy)
	require.Nil(t, back.ClaimedAt)
}

func TestStewardCanForceTransition(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	rec := transition(t, h, b.ID.String(), stewardID, map[string]any{"to": "OPEN"})
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestStewardCanClaimDirectedBountyForOthers exercises the one capability that
// authorizeTransition's steward short-circuit uniquely grants: a steward can
// move a DIRECTED bounty into CLAIMED even though it is directed at someone
// else, bypassing domain.CanClaim's ErrNotDirectedToYou. TestStewardCanForceTransition
// alone would still pass if the short-circuit were deleted, because
// domain.CanEdit already grants stewards the DRAFT->OPEN edge; this test fails
// if the short-circuit is removed.
func TestStewardCanClaimDirectedBountyForOthers(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type":        "DELIVERY",
		"title":       "定向单:仅研发C可认领",
		"visibility":  "DIRECTED",
		"directed_to": engCID,
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)

	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)

	rec = transition(t, h, b.ID.String(), stewardID, map[string]any{"to": "CLAIMED"})
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestThirdPartyCannotTransitionOthersBounty covers the case where the actor is
// neither the sponsor, a steward, nor the claimer.
func TestThirdPartyCannotTransitionOthersBounty(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)

	rec := transition(t, h, b.ID.String(), engDID, map[string]any{"to": "DELIVERED"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestClaimerCannotAcceptOwnDelivery pins the A1 fix: the deliverer must not be
// able to accept their own work into COMPLETED. Only the sponsor (or a
// steward) may.
func TestClaimerCannotAcceptOwnDelivery(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED"}).Code)

	rec := transition(t, h, b.ID.String(), engCID, map[string]any{"to": "COMPLETED"})
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED"})
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestClaimerRetainsOtherEdges pins that the A1 fix is scoped to COMPLETED
// only: the claimer can still hand a DELIVERED bounty back to CLAIMED, and can
// still abandon their own bounty with a retrospective.
func TestClaimerRetainsOtherEdges(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED"}).Code)

	rec := transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"})
	require.Equal(t, http.StatusOK, rec.Code)

	rec = transition(t, h, b.ID.String(), engCID, map[string]any{
		"to":            "ABANDONED",
		"retrospective": "接口联调受阻,claimer 主动放弃",
	})
	require.Equal(t, http.StatusOK, rec.Code)
}
