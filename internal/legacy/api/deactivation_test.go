package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	legacyapi "bountyboard/internal/legacy/api"
	"bountyboard/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

// TestDeactivatedUserRefusedByAPI pins the semantics settled on for
// UserStore.SetActive / withIdentity: "active" governs who may act. A
// deactivated identity is refused by the one place identity is resolved, for
// both a mutating call (create) and a plain read (list) — a person who has
// left has no standing to ask the system anything, not just to change it.
func TestDeactivatedUserRefusedByAPI(t *testing.T) {
	h := newTestServer(t)
	deactivateUser(t, engCID)

	rec := do(t, h, http.MethodGet, "/api/legacy/bounties", engCID, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, "a deactivated user must be refused even a read")

	rec = do(t, h, http.MethodPost, "/api/legacy/bounties", engCID, map[string]any{
		"type": "DELIVERY", "title": "deactivated user tries to open a bounty",
	})
	require.Equal(t, http.StatusForbidden, rec.Code, "a deactivated user must be refused a write")
}

// TestDeactivatedUserCreditStillRendersName pins the other half of the
// decision: "active" never governs who is REMEMBERED. Once someone is
// credited and confirmed on a work, deactivating them afterwards must not
// blank their name out of the feed or their own portfolio — the archive's
// whole point is that the trace survives someone leaving the team.
func TestDeactivatedUserCreditStillRendersName(t *testing.T) {
	h := newTestServer(t)
	b := completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	deactivateUser(t, engDID)

	feedRec := do(t, h, http.MethodGet, "/api/legacy/works", pmID, nil)
	require.Equal(t, http.StatusOK, feedRec.Code)
	var feed []legacyapi.WorkView
	require.NoError(t, json.Unmarshal(feedRec.Body.Bytes(), &feed))
	work := findWork(feed, b.ID)
	require.NotNil(t, work)
	require.Len(t, work.Credits, 1)
	require.Equal(t, "研发 D", work.Credits[0].UserName,
		"a deactivated nominee's real name must still render in the feed")

	portRec := do(t, h, http.MethodGet, "/api/legacy/users/"+engDID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, portRec.Code)
	var portfolio []legacyapi.WorkView
	require.NoError(t, json.Unmarshal(portRec.Body.Bytes(), &portfolio))
	require.Len(t, portfolio, 1)
	require.Len(t, portfolio[0].Credits, 1)
	require.Equal(t, "研发 D", portfolio[0].Credits[0].UserName,
		"a deactivated user's own portfolio must still render their real name")
	require.Equal(t, domain.CreditRoleCoDeliver, portfolio[0].Credits[0].Credit.Role)
}
