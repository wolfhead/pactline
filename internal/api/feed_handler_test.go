package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"bountyboard/internal/api"
	"bountyboard/internal/domain"

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

func TestPortfolioOfUncreditedUserIsEmpty(t *testing.T) {
	h := newTestServer(t)
	completeWithCredit(t, h, engDID, "CO_DELIVER", "")

	rec := do(t, h, http.MethodGet, "/api/users/"+stewardID+"/portfolio", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var works []api.WorkView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &works))
	require.Empty(t, works)
}
