package api

import (
	"errors"
	"fmt"
	"testing"

	userdomain "github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/legacy/domain"

	"github.com/stretchr/testify/require"
)

// TestIsAlreadySettledErrorClassifiesSettleFailures pins I1's fix at the
// exact decision point the settlement handler uses to bucket a Settle
// failure: only domain.ErrAlreadySettled (directly or wrapped) belongs in
// AlreadySettledCount. Every other error — a dropped connection, a
// constraint violation, a cancelled context, or any other genuine
// infrastructure failure — must be classified as a real failure, never
// silently folded into the benign "someone already settled this" count.
//
// This is a white-box test (package api, not api_test) because
// isAlreadySettledError is unexported: it exists specifically so this
// classification can be pinned directly, since reliably forcing a real
// Postgres write failure mid-settlement-run through the HTTP surface would
// be either non-deterministic (timing-dependent) or would require injecting
// a fault the store layer has no seam for. See
// internal/legacy/store/scoring_store_test.go's
// TestSettleReturnsGenuineErrorDistinctFromAlreadySettled for the
// complementary proof that Settle itself CAN fail with something other than
// domain.ErrAlreadySettled.
func TestIsAlreadySettledErrorClassifiesSettleFailures(t *testing.T) {
	require.True(t, isAlreadySettledError(domain.ErrAlreadySettled))
	require.True(t, isAlreadySettledError(fmt.Errorf("settle: %w", domain.ErrAlreadySettled)),
		"a wrapped ErrAlreadySettled must still classify as already-settled")

	require.False(t, isAlreadySettledError(errors.New("connection reset by peer")),
		"a genuine infrastructure failure must NOT be classified as already-settled")
	require.False(t, isAlreadySettledError(errors.New("context canceled")))
	require.False(t, isAlreadySettledError(userdomain.ErrNotFound),
		"an unrelated domain error must not be misclassified as already-settled")
}
