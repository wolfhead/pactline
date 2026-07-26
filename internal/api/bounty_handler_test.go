package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"bountyboard"
	"bountyboard/internal/api"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	pmID   = "00000000-0000-0000-0000-000000000001"
	engCID = "00000000-0000-0000-0000-000000000003"
)

type httptestRecorder = httptest.ResponseRecorder

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; run via `make test`")
	}
	db, err := store.Connect(context.Background(), dsn)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), bountyboard.MigrationFS))
	t.Cleanup(db.Close)

	return api.NewRouter(store.NewUserStore(db), store.NewBountyStore(db), store.NewCreditStore(db))
}

func do(t *testing.T, h http.Handler, method, path, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// cleanupBounty deletes a bounty created through the HTTP layer once the
// test finishes.
//
// The suite runs against a shared, already-migrated database whose rows
// persist across test runs and are visible to the internal/store package's
// tests running concurrently in the same `go test ./...` invocation (see
// internal/store/bounty_store_test.go's cleanupBounties, which documents and
// handles the same constraint). Without this, TestCreateBountyDefaultsToDraft
// leaves a DELIVERY bounty sponsored by pmID behind, which is exactly the
// shape several store-package tests filter on and assert an exact count for
// — this keeps this test responsible for the row it created instead of
// weakening those assertions.
func cleanupBounty(t *testing.T, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return
		}
		db, err := store.Connect(context.Background(), dsn)
		require.NoError(t, err)
		defer db.Close()
		_, err = db.Pool.Exec(context.Background(), `DELETE FROM bounties WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

func TestListUsersReturnsSeeded(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/users", pmID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var users []domain.User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &users))
	require.Len(t, users, 6)
}

func TestMissingIdentityIsUnauthorized(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties", "", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateBountyDefaultsToDraft(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type":       "DELIVERY",
		"title":      "竞价链路降延迟",
		"goal":       "P99 80ms → 45ms",
		"commitment": "COMMITTED",
		"business_lines": []map[string]any{
			{"tag": "DSP", "weight": 0.7},
			{"tag": "ADX", "weight": 0.3},
		},
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)
	require.Equal(t, domain.StatusDraft, b.Status)
	require.Equal(t, domain.VisibilityPublic, b.Visibility)
	require.Equal(t, pmID, b.SponsorID.String())

	got := do(t, h, http.MethodGet, "/api/bounties/"+b.ID.String(), engCID, nil)
	require.Equal(t, http.StatusOK, got.Code)
}

func TestGetUnknownBountyIs404(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties/11111111-1111-1111-1111-111111111111", pmID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestCreateBountyDefaultsAllFields exercises the Type, Commitment and
// Visibility defaulting branches in create by omitting all three from the
// request body, so deleting any of those `if` blocks fails this test.
func TestCreateBountyDefaultsAllFields(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"title": "缺省字段开单",
		"goal":  "验证默认值",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)

	require.Equal(t, domain.BountyTypeDelivery, b.Type)
	require.Equal(t, domain.CommitmentCommitted, b.Commitment)
	require.Equal(t, domain.VisibilityPublic, b.Visibility)
}

// TestCreateBountyWeightMismatchStillSucceeds locks in the warn-not-reject
// behaviour for business line weights that do not sum to 1: a future change
// to reject such requests must break this test explicitly.
func TestCreateBountyWeightMismatchStillSucceeds(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type":       "DELIVERY",
		"title":      "权重不为一也能建单",
		"commitment": "COMMITTED",
		"business_lines": []map[string]any{
			{"tag": "DSP", "weight": 0.5},
			{"tag": "ADX", "weight": 0.2},
		},
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)
}

// TestCreateBountyRequiresSponsorOrSteward pins the fix for the authorisation
// gap where any authenticated identity, regardless of role, could create a
// bounty (and thus be attributed as sponsor_id) through the HTTP API even
// though the Board page only shows the form to sponsors and stewards.
func TestCreateBountyRequiresSponsorOrSteward(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", engCID, map[string]any{
		"type":  "DELIVERY",
		"title": "工程师尝试直接建单",
		"goal":  "绕过前端隐藏的开单入口",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateBountyAllowsSponsor(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", pmID, map[string]any{
		"type":  "DELIVERY",
		"title": "产品 A 建单",
		"goal":  "验证 SPONSOR 仍可开单",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)
}

func TestCreateBountyAllowsSteward(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodPost, "/api/bounties", stewardID, map[string]any{
		"type":  "DELIVERY",
		"title": "Steward F 建单",
		"goal":  "验证 STEWARD 仍可开单",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	var b domain.Bounty
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	cleanupBounty(t, b.ID)
}

func TestMalformedIdentityIsUnauthorized(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties", "not-a-uuid", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUnknownIdentityIsUnauthorized(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties", "99999999-9999-9999-9999-999999999999", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetBountyWithNonUUIDIdIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties/not-a-uuid", pmID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListWithMalformedClaimedByIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties?claimed_by=not-a-uuid", pmID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListWithUnknownStatusIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties?status=OPEM", pmID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListWithUnknownTypeIsBadRequest(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/api/bounties?type=BOGUS", pmID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
