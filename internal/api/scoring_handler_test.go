package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"bountyboard/internal/domain"

	"github.com/stretchr/testify/require"
)

const techLeadID = "00000000-0000-0000-0000-000000000002" // 技术 Leader B: SPONSOR + TECH_LEAD

// setValueLevel POSTs to /api/bounties/{id}/value-level as actor.
func setValueLevel(t *testing.T, h http.Handler, id, actor string, level string) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/bounties/"+id+"/value-level", actor, map[string]any{"value_level": level})
}

// setDifficulty POSTs to /api/bounties/{id}/difficulty as actor.
func setDifficulty(t *testing.T, h http.Handler, id, actor string, level string) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/bounties/"+id+"/difficulty", actor, map[string]any{"difficulty": level})
}

// TestSponsorSetsValueLevelAtCreateAndAmendsWhileOpen pins spec §6.1: the
// sponsor sets the value level at creation and may amend it while the
// bounty is still DRAFT/OPEN.
func TestSponsorSetsValueLevelAtCreateAndAmendsWhileOpen(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type": "DELIVERY", "title": "定价档单", "value_level": "A",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)
	require.Equal(t, domain.ValueA, b.ValueLevel)

	rec = setValueLevel(t, h, b.ID.String(), pmID, "S")
	require.Equal(t, http.StatusOK, rec.Code)
	var amended domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &amended))
	require.Equal(t, domain.ValueS, amended.ValueLevel)
}

// TestValueLevelLockedOnceClaimed pins the DRAFT/OPEN window: once a bounty
// is claimed, the sponsor may no longer amend its value level.
func TestValueLevelLockedOnceClaimed(t *testing.T) {
	h := newTestServer(t)
	b := claimedBounty(t, h)

	rec := setValueLevel(t, h, b.ID.String(), pmID, "S")
	require.Equal(t, http.StatusConflict, rec.Code)
}

// TestStrangerCannotSetValueLevel covers a non-sponsor, non-steward actor.
func TestStrangerCannotSetValueLevel(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := setValueLevel(t, h, b.ID.String(), engCID, "A")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestInvalidValueLevelIsBadRequest pins input validation before the
// authorisation/status checks apply.
func TestInvalidValueLevelIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := setValueLevel(t, h, b.ID.String(), pmID, "Z")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestTechLeadSetsDifficultyAndSponsorCannot is one of the task's explicit
// required regressions: "a sponsor cannot set difficulty and a tech lead
// can". pmID holds only SPONSOR; techLeadID holds SPONSOR and TECH_LEAD, so
// it also proves that holding TECH_LEAD alongside SPONSOR is what grants the
// capability, not sponsorship of this particular bounty.
func TestTechLeadSetsDifficultyAndSponsorCannot(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := setDifficulty(t, h, b.ID.String(), pmID, "M")
	require.Equal(t, http.StatusForbidden, rec.Code, "a sponsor, even the bounty's own sponsor, must never set difficulty")

	rec = setDifficulty(t, h, b.ID.String(), techLeadID, "M")
	require.Equal(t, http.StatusOK, rec.Code)
	var updated domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.Equal(t, domain.DifficultyM, updated.Difficulty)
}

// TestStewardSetsDifficulty pins the STEWARD half of the role gate.
func TestStewardSetsDifficulty(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := setDifficulty(t, h, b.ID.String(), stewardID, "XL")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestInvalidDifficultyIsBadRequest pins input validation.
func TestInvalidDifficultyIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := setDifficulty(t, h, b.ID.String(), techLeadID, "HUGE")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// gradedBounty drives a bounty through the full happy path to COMPLETED with
// value level, difficulty and completion all set, so scoring.Score can
// compute a real number on it. Returns the completed bounty.
func gradedBounty(t *testing.T, h http.Handler, value, difficulty, completion string) domain.Bounty {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type": "DELIVERY", "title": "计分闭环单", "value_level": value,
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)

	require.Equal(t, http.StatusOK, setDifficulty(t, h, b.ID.String(), techLeadID, difficulty).Code)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), engCID, map[string]any{"to": "DELIVERED"}).Code)
	rec = transition(t, h, b.ID.String(), pmID, map[string]any{"to": "COMPLETED", "completion": completion})
	require.Equal(t, http.StatusOK, rec.Code)
	var done domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &done))
	return done
}

// settle POSTs to /api/settlements as actor over [from, to].
func settle(t *testing.T, h http.Handler, actor string, from, to time.Time) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/settlements", actor, map[string]any{
		"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339),
	})
}

type settlementResponseView struct {
	Settled []struct {
		BountyID string  `json:"bounty_id"`
		Score    float64 `json:"score"`
	} `json:"settled"`
	SettledCount        int `json:"settled_count"`
	AlreadySettledCount int `json:"already_settled_count"`
	Unscorable          []struct {
		BountyID string `json:"bounty_id"`
		Reason   string `json:"reason"`
	} `json:"unscorable"`
	UnscorableCount int `json:"unscorable_count"`
}

// TestNonStewardCannotSettle is one of the task's explicit required
// regressions.
func TestNonStewardCannotSettle(t *testing.T) {
	h := newTestServer(t)
	rec := settle(t, h, pmID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestSettlementScoresSkipsAlreadySettledAndReportsUnscorable is the task's
// explicit required regression: "settlement skips already-settled and
// unscorable records and says so."
func TestSettlementScoresSkipsAlreadySettledAndReportsUnscorable(t *testing.T) {
	h := newTestServer(t)
	from := time.Now().Add(-time.Hour)

	graded := gradedBounty(t, h, "A", "M", "MET") // A(5) x M(2) x MET(1.0) x COMMITTED(1.0) = 10

	unscorable := createDraft(t, h) // never graded
	require.Equal(t, http.StatusOK, transition(t, h, unscorable.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, unscorable.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, unscorable.ID.String(), engCID, map[string]any{"to": "DELIVERED"}).Code)
	rec := transition(t, h, unscorable.ID.String(), pmID, map[string]any{"to": "COMPLETED"})
	require.Equal(t, http.StatusOK, rec.Code)

	to := time.Now().Add(time.Hour)

	// First run: settles the graded bounty, reports the unscorable one.
	rec = settle(t, h, stewardID, from, to)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp settlementResponseView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.SettledCount)
	require.Equal(t, graded.ID.String(), resp.Settled[0].BountyID)
	require.InDelta(t, 10, resp.Settled[0].Score, 1e-9)
	require.Equal(t, 1, resp.UnscorableCount)
	require.Equal(t, unscorable.ID.String(), resp.Unscorable[0].BountyID)
	require.NotEmpty(t, resp.Unscorable[0].Reason)

	got := do(t, h, http.MethodGet, "/api/bounties/"+graded.ID.String(), pmID, nil)
	require.Equal(t, http.StatusOK, got.Code)
	var settledBounty domain.Bounty
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &settledBounty))
	require.NotNil(t, settledBounty.SettledScore)
	require.InDelta(t, 10, *settledBounty.SettledScore, 1e-9)
	require.NotNil(t, settledBounty.SettledAt)

	// Second run over the same period: the graded bounty is now
	// already-settled and must be skipped, not rescored; the unscorable
	// bounty is still unscorable and reported again. A botched run can
	// simply be re-run.
	rec = settle(t, h, stewardID, from, to)
	require.Equal(t, http.StatusOK, rec.Code)
	var second settlementResponseView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	require.Equal(t, 0, second.SettledCount)
	require.Equal(t, 1, second.AlreadySettledCount)
	require.Equal(t, 1, second.UnscorableCount)
}

// TestSettlementDoesNotTouchAbandonedOrOtherPeriods pins that only terminal
// bounties (COMPLETED/ABANDONED) inside the requested window are candidates,
// and covers the ABANDONED scoring branch through the settlement endpoint
// end to end.
func TestSettlementScoresAbandonedBounty(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)
	// Value level must be set while still DRAFT/OPEN, before the bounty is
	// claimed (see TestValueLevelLockedOnceClaimed) — order matters here.
	require.Equal(t, http.StatusOK, setValueLevel(t, h, b.ID.String(), pmID, "A").Code)
	require.Equal(t, http.StatusOK, setDifficulty(t, h, b.ID.String(), techLeadID, "M").Code)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)
	require.Equal(t, http.StatusOK, transition(t, h, b.ID.String(), engCID, map[string]any{"to": "CLAIMED"}).Code)
	rec := transition(t, h, b.ID.String(), engCID, map[string]any{
		"to": "ABANDONED", "retrospective": "接口未就绪,先归档",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	rec = settle(t, h, stewardID, from, to)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp settlementResponseView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	var found bool
	for _, s := range resp.Settled {
		if s.BountyID == b.ID.String() {
			found = true
			// COMMITTED (default commitment) abandoned bounty scores 0 per
			// spec §7.1.1.
			require.InDelta(t, 0, s.Score, 1e-9)
		}
	}
	require.True(t, found, "the abandoned bounty must be scored and reported settled")
}

// TestSettlementMissingFromOrToIsBadRequest pins basic input validation.
func TestSettlementMissingFromOrToIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/settlements", stewardID, map[string]any{})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// createCalibration POSTs to /api/bounties/{id}/calibrations as actor.
func createCalibration(t *testing.T, h http.Handler, id, actor string, body map[string]any) *httptestRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/api/bounties/"+id+"/calibrations", actor, body)
}

// TestCalibrationOverridesWithoutDestroyingSnapshot is the task's explicit
// required regression: "a calibration overrides without destroying the
// snapshot."
func TestCalibrationOverridesWithoutDestroyingSnapshot(t *testing.T) {
	h := newTestServer(t)
	graded := gradedBounty(t, h, "A", "M", "MET") // settles at 10

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	require.Equal(t, http.StatusOK, settle(t, h, stewardID, from, to).Code)

	rec := createCalibration(t, h, graded.ID.String(), stewardID, map[string]any{
		"quarter": "2026Q3", "calibrated_value": "B", "note": "实际毛利未达预期,下调一档",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var cal domain.Calibration
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cal))
	require.Equal(t, domain.ValueA, cal.OriginalValue, "original_value must be captured from the bounty, not the request")
	require.Equal(t, domain.ValueB, cal.CalibratedValue)
	require.InDelta(t, 6, cal.CalibratedScore, 1e-9, "B(3) x M(2) x MET(1.0) x COMMITTED(1.0) = 6")

	// The snapshot must be untouched by the calibration.
	got := do(t, h, http.MethodGet, "/api/bounties/"+graded.ID.String(), pmID, nil)
	require.Equal(t, http.StatusOK, got.Code)
	var afterCalibration domain.Bounty
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &afterCalibration))
	require.NotNil(t, afterCalibration.SettledScore)
	require.InDelta(t, 10, *afterCalibration.SettledScore, 1e-9,
		"calibration must override, not mutate, the settlement snapshot")

	list := do(t, h, http.MethodGet, "/api/bounties/"+graded.ID.String()+"/calibrations", pmID, nil)
	require.Equal(t, http.StatusOK, list.Code)
	var cals []domain.Calibration
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &cals))
	require.Len(t, cals, 1)
	require.Equal(t, cal.ID, cals[0].ID)
}

// TestCalibrationRequiresSettlement pins that calibration is a post-
// settlement correction, not a way to pre-empt the snapshot.
func TestCalibrationRequiresSettlement(t *testing.T) {
	h := newTestServer(t)
	graded := gradedBounty(t, h, "A", "M", "MET")

	rec := createCalibration(t, h, graded.ID.String(), stewardID, map[string]any{
		"quarter": "2026Q3", "calibrated_value": "B",
	})
	require.Equal(t, http.StatusConflict, rec.Code)
}

// TestNonStewardCannotCalibrate pins that calibration cannot be the
// sponsor's own call — see calibration_handler.go's doc comment for why.
func TestNonStewardCannotCalibrate(t *testing.T) {
	h := newTestServer(t)
	graded := gradedBounty(t, h, "A", "M", "MET")
	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	require.Equal(t, http.StatusOK, settle(t, h, stewardID, from, to).Code)

	rec := createCalibration(t, h, graded.ID.String(), pmID, map[string]any{
		"quarter": "2026Q3", "calibrated_value": "B",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestAnchorCRUDViaHTTP exercises the full anchor lifecycle through the
// HTTP surface: steward-only create/update/delete, unrestricted read.
func TestAnchorCRUDViaHTTP(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h)

	rec := do(t, h, http.MethodPost, "/api/anchors", engCID, map[string]any{
		"dimension": "VALUE", "level": "A", "bounty_id": b.ID.String(),
	})
	require.Equal(t, http.StatusForbidden, rec.Code, "a non-steward must not create an anchor")

	rec = do(t, h, http.MethodPost, "/api/anchors", stewardID, map[string]any{
		"dimension": "VALUE", "level": "A", "bounty_id": b.ID.String(), "note": "先例",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var anchor domain.AnchorExample
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &anchor))

	listRec := do(t, h, http.MethodGet, "/api/anchors?dimension=VALUE&level=A", engCID, nil)
	require.Equal(t, http.StatusOK, listRec.Code)
	var anchors []domain.AnchorExample
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &anchors))
	require.Len(t, anchors, 1)

	updRec := do(t, h, http.MethodPut, "/api/anchors/"+anchor.ID.String(), stewardID, map[string]any{
		"level": "S", "note": "重新定档",
	})
	require.Equal(t, http.StatusOK, updRec.Code)
	var updated domain.AnchorExample
	require.NoError(t, json.Unmarshal(updRec.Body.Bytes(), &updated))
	require.Equal(t, "S", updated.Level)

	delRec := do(t, h, http.MethodDelete, "/api/anchors/"+anchor.ID.String(), engCID, nil)
	require.Equal(t, http.StatusForbidden, delRec.Code, "a non-steward must not delete an anchor")

	delRec = do(t, h, http.MethodDelete, "/api/anchors/"+anchor.ID.String(), stewardID, nil)
	require.Equal(t, http.StatusNoContent, delRec.Code)
}
