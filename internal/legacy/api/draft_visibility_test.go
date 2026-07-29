package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// listBounties GETs /api/bounties as actor and decodes the result.
func listBounties(t *testing.T, h http.Handler, actor string) []domain.Bounty {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/legacy/bounties", actor, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var out []domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func containsBounty(list []domain.Bounty, id uuid.UUID) bool {
	for _, b := range list {
		if b.ID == id {
			return true
		}
	}
	return false
}

// TestDraftInvisibleToNonSponsorViaListAndGet pins spec §5's "DRAFT 仅出题人
// 可见" through both read paths. A non-sponsor, non-steward engineer must see
// neither the draft in the list nor be able to fetch it directly (404, not
// 403 — see canViewDraft's comment in bounty_handler.go). The sponsor and a
// steward see it through both paths.
func TestDraftInvisibleToNonSponsorViaListAndGet(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h) // sponsored by pmID

	require.False(t, containsBounty(listBounties(t, h, engCID), b.ID),
		"a non-sponsor engineer must not see another sponsor's draft in the list")
	require.Equal(t, http.StatusNotFound,
		do(t, h, http.MethodGet, "/api/legacy/bounties/"+b.ID.String(), engCID, nil).Code,
		"a non-sponsor engineer must get 404, not 403, fetching another sponsor's draft directly")

	require.True(t, containsBounty(listBounties(t, h, pmID), b.ID),
		"the sponsor must see their own draft in the list")
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodGet, "/api/legacy/bounties/"+b.ID.String(), pmID, nil).Code,
		"the sponsor must be able to fetch their own draft directly")

	require.True(t, containsBounty(listBounties(t, h, stewardID), b.ID),
		"a steward must see any draft in the list")
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodGet, "/api/legacy/bounties/"+b.ID.String(), stewardID, nil).Code,
		"a steward must be able to fetch any draft directly")
}

// TestNonDraftBountyStaysVisibleToEveryone guards against an over-broad fix:
// the draft predicate must touch DRAFT rows only. Once the bounty leaves
// DRAFT, a non-sponsor engineer must see it through both list and get exactly
// as before.
func TestNonDraftBountyStaysVisibleToEveryone(t *testing.T) {
	h := newTestServer(t)
	b := createDraft(t, h) // sponsored by pmID
	require.Equal(t, http.StatusOK,
		transition(t, h, b.ID.String(), pmID, map[string]any{"to": "OPEN"}).Code)

	require.True(t, containsBounty(listBounties(t, h, engCID), b.ID),
		"an OPEN bounty must remain visible to a non-sponsor engineer in the list")
	require.Equal(t, http.StatusOK,
		do(t, h, http.MethodGet, "/api/legacy/bounties/"+b.ID.String(), engCID, nil).Code,
		"an OPEN bounty must remain fetchable by a non-sponsor engineer")
}
