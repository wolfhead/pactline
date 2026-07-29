package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	legacyapi "github.com/wolfhead/pactline/internal/legacy/api"
	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

// TestPhase1FullLoop walks the whole mechanism end to end: a plan bounty is
// defined, a delivery bounty inherits its author as DEFINE, collaborators are
// credited and confirm, an unrelated bounty is abandoned with its
// retrospective, and the finished works appear in the feed with exactly the
// confirmed credits.
//
// Two authorisation rules changed during review after this test's shape was
// specced and are pinned here rather than contradicted:
//   - POST /api/legacy/bounties requires SPONSOR or STEWARD, so both bounties below
//     are opened by pmID (产品 A, a sponsor), never by an ENGINEER-only actor.
//   - DELIVERED -> COMPLETED requires the sponsor or a steward, never the
//     claimer, so every COMPLETED transition below is driven by pmID.
func TestPhase1FullLoop(t *testing.T) {
	const engEID = "00000000-0000-0000-0000-000000000005" // 研发 E

	h := newTestServer(t)

	// 1. 产品 A opens a plan bounty; 研发 C claims, delivers and it is accepted.
	planRec := do(t, h, http.MethodPost, "/api/legacy/bounties", pmID, map[string]any{
		"type": "PLAN", "title": "降低竞价链路延迟的技术方案",
		"goal":                "给出把 P99 压到 50ms 以内的方案与里程碑",
		"acceptance_criteria": "方案评审通过,含里程碑与回滚策略",
		"business_lines":      []map[string]any{{"tag": "DSP", "weight": 1}},
	})
	require.Equal(t, http.StatusCreated, planRec.Code)
	var plan domain.Bounty
	require.NoError(t, json.Unmarshal(planRec.Body.Bytes(), &plan))
	// Registered before the delivery's cleanup below. t.Cleanup runs LIFO, so
	// this parent-plan deletion executes SECOND, after the child delivery row
	// is already gone. bounties.parent_id has a foreign key with no cascade,
	// so deleting the parent first would fail teardown. Do not reorder these
	// two cleanupBounty calls, and do not factor this creation into a shared
	// helper without preserving the registration order.
	cleanupBounty(t, plan.ID)

	require.Equal(t, http.StatusOK, transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, plan.ID.String(), engCID, map[string]any{"to": "DELIVERED", "person_days": 2}).Code)
	// Acceptance is the sponsor's call, not the claimer's: 产品 A (pmID), not
	// 研发 C, moves the plan into COMPLETED.
	require.Equal(t, http.StatusOK, transition(t, h, plan.ID.String(), pmID, map[string]any{"to": "COMPLETED"}).Code)

	// 2. A delivery bounty descends from it and inherits 研发 C as DEFINE.
	delRec := do(t, h, http.MethodPost, "/api/legacy/bounties", pmID, map[string]any{
		"type": "DELIVERY", "title": "按方案实现竞价链路降延迟",
		"parent_id":      plan.ID.String(),
		"business_lines": []map[string]any{{"tag": "DSP", "weight": 0.7}, {"tag": "ADX", "weight": 0.3}},
	})
	require.Equal(t, http.StatusCreated, delRec.Code)
	var delivery domain.Bounty
	require.NoError(t, json.Unmarshal(delRec.Body.Bytes(), &delivery))
	// Registered after the plan's cleanup above, so it runs FIRST (LIFO):
	// the child delivery row is deleted before its parent plan row.
	cleanupBounty(t, delivery.ID)

	inherited := listCredits(t, h, delivery.ID.String())
	require.Len(t, inherited, 1)
	require.Equal(t, domain.CreditRoleDefine, inherited[0].Role)
	require.Equal(t, engCID, inherited[0].UserID.String())
	require.Nil(t, inherited[0].NominatedBy, "the system inherits the credit but never confirms on anyone's behalf")
	require.Equal(t, domain.CreditPending, inherited[0].Status)

	// 研发 C confirms the inherited credit themselves.
	require.Equal(t, http.StatusOK, do(t, h, http.MethodPost,
		"/api/legacy/credits/"+inherited[0].ID.String()+"/respond", engCID,
		map[string]any{"status": "CONFIRMED"}).Code)

	// 3. 研发 D claims and delivers, crediting 研发 C for support.
	require.Equal(t, http.StatusOK, transition(t, h, delivery.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, delivery.ID.String(), engDID, map[string]any{"to": "CLAIMED"}).Code)

	supRec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+delivery.ID.String()+"/credits", engDID,
		map[string]any{"user_id": engCID, "role": "SUPPORT"})
	require.Equal(t, http.StatusCreated, supRec.Code)
	var support domain.Credit
	require.NoError(t, json.Unmarshal(supRec.Body.Bytes(), &support))
	require.Equal(t, http.StatusOK, do(t, h, http.MethodPost,
		"/api/legacy/credits/"+support.ID.String()+"/respond", engCID,
		map[string]any{"status": "CONFIRMED"}).Code)

	// 研发 D also brings in 研发 E as a co-deliverer, nominated after the
	// SUPPORT credit above. This makes the feed's ordering assertion below
	// discriminating: creation order is DEFINE, SUPPORT, CO_DELIVER and
	// alphabetical order is CO_DELIVER, DEFINE, SUPPORT, but the mechanism's
	// display order (by part) is DEFINE, CO_DELIVER, SUPPORT — a sequence
	// that agrees with neither.
	coDelivRec := do(t, h, http.MethodPost, "/api/legacy/bounties/"+delivery.ID.String()+"/credits", engDID,
		map[string]any{"user_id": engEID, "role": "CO_DELIVER"})
	require.Equal(t, http.StatusCreated, coDelivRec.Code)
	var coDeliver domain.Credit
	require.NoError(t, json.Unmarshal(coDelivRec.Body.Bytes(), &coDeliver))
	require.Equal(t, http.StatusOK, do(t, h, http.MethodPost,
		"/api/legacy/credits/"+coDeliver.ID.String()+"/respond", engEID,
		map[string]any{"status": "CONFIRMED"}).Code)

	// A credit that is nominated but never confirmed must not appear anywhere
	// in the feed.
	require.Equal(t, http.StatusCreated, do(t, h, http.MethodPost,
		"/api/legacy/bounties/"+delivery.ID.String()+"/credits", engDID,
		map[string]any{"user_id": pmID, "role": "SUPPORT"}).Code)

	require.Equal(t, http.StatusOK, transition(t, h, delivery.ID.String(), engDID,
		map[string]any{"to": "DELIVERED", "person_days": 12}).Code)
	// Again the sponsor, not the claimer (engDID), accepts the delivery.
	require.Equal(t, http.StatusOK, transition(t, h, delivery.ID.String(), pmID,
		map[string]any{"to": "COMPLETED"}).Code)

	// 4. An unrelated bounty is abandoned with a retrospective. This is the
	// other half of the mechanism: archiving failure with its conclusion is
	// what makes hard problems safe to attempt, and it must survive into the
	// feed exactly like a completed work does.
	abandoned := claimedBounty(t, h)
	require.Equal(t, http.StatusOK, transition(t, h, abandoned.ID.String(), engCID, map[string]any{
		"to":            "ABANDONED",
		"retrospective": "上游 SSP 接口未按期提供,结论:先做 mock 层再重开",
	}).Code)

	// 5. The feed shows all three works: the plan, the delivery (with
	// exactly its three confirmed credits, ordered by part rather than by
	// creation order or alphabetically), and the abandoned bounty carrying
	// its retrospective.
	feedRec := do(t, h, http.MethodGet, "/api/legacy/works", pmID, nil)
	require.Equal(t, http.StatusOK, feedRec.Code)
	var feed []legacyapi.WorkView
	require.NoError(t, json.Unmarshal(feedRec.Body.Bytes(), &feed))
	require.Len(t, feed, 3)

	var deliveryView, abandonedView *legacyapi.WorkView
	for i := range feed {
		switch feed[i].Bounty.ID {
		case delivery.ID:
			deliveryView = &feed[i]
		case abandoned.ID:
			abandonedView = &feed[i]
		}
	}
	require.NotNil(t, deliveryView)
	require.Len(t, deliveryView.Credits, 3, "only the three confirmed credits surface; the pending SUPPORT for pmID does not")
	// Ordered by part (DEFINE, LEAD, CO_DELIVER, REVIEW, SUPPORT, BASELINE),
	// not by creation order (DEFINE, SUPPORT, CO_DELIVER) and not
	// alphabetically (CO_DELIVER, DEFINE, SUPPORT) — this sequence agrees
	// with neither, so it actually exercises the sort in decorate rather
	// than passing by coincidence.
	gotRoles := make([]domain.CreditRole, len(deliveryView.Credits))
	for i, cv := range deliveryView.Credits {
		gotRoles[i] = cv.Credit.Role
	}
	require.Equal(t,
		[]domain.CreditRole{domain.CreditRoleDefine, domain.CreditRoleCoDeliver, domain.CreditRoleSupport},
		gotRoles, "credits are ordered by part")
	require.Equal(t, "研发 C", deliveryView.Credits[0].UserName)
	require.Equal(t, "研发 E", deliveryView.Credits[1].UserName)
	require.Equal(t, "研发 C", deliveryView.Credits[2].UserName)

	require.NotNil(t, abandonedView, "an abandoned bounty must appear in the feed")
	require.Equal(t, domain.StatusAbandoned, abandonedView.Bounty.Status)
	require.Equal(t, "上游 SSP 接口未按期提供,结论:先做 mock 层再重开", abandonedView.Bounty.Retrospective,
		"failure archived without its conclusion is half the mechanism gone")
	require.Empty(t, abandonedView.Credits)

	// 6. 研发 C's portfolio spans only the work they hold a confirmed credit
	// on: the delivery. The plan bounty has no confirmed credit on it (研发 C
	// was never credited on the plan itself, only inherited forward onto its
	// child), and the abandoned bounty carries no credits at all.
	portRec := do(t, h, http.MethodGet, "/api/legacy/users/"+engCID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, portRec.Code)
	var portfolio []legacyapi.WorkView
	require.NoError(t, json.Unmarshal(portRec.Body.Bytes(), &portfolio))
	require.Len(t, portfolio, 1, "the plan bounty has no confirmed credit yet, only the delivery does")
	require.Equal(t, delivery.ID, portfolio[0].Bounty.ID)
}

func listCredits(t *testing.T, h http.Handler, bountyID string) []domain.Credit {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/legacy/bounties/"+bountyID+"/credits", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var out []domain.Credit
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}
