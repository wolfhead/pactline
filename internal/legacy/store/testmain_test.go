package store_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wolfhead/pactline"
	pgstore "github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

// skippedForNoDatabase, testDSN, TestMain and newTestDB below are local
// copies of the identical helpers in internal/store/postgres_test.go and
// internal/store/user_store_test.go. This package's test files (moved here
// from internal/store as part of the legacy split — see
// internal/legacy/README.md) can no longer reach unexported package-level
// helpers defined in that other package, and `go test` recognizes only one
// TestMain per package, so each package keeps its own.
var skippedForNoDatabase atomic.Int64

// testDSN returns the shared test database DSN, or stops the calling test.
//
// A3/A5 concern: `go test ./...` run bare (no DATABASE_URL) still reports
// PASS if every store test just skips — the mandated regression guards for
// this package (bounty/credit/calibration/anchor persistence) would silently
// vanish. Under CI this must be a hard failure rather than a quiet skip,
// since a green CI run is exactly the signal a maintainer trusts without
// looking closer. Locally, where a bare `go test ./...` during development
// is normal, skip but count it so TestMain can shout about the shortfall on
// exit.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL not set while CI is set: refusing to silently skip postgres integration tests. Run via `make test`.")
		}
		skippedForNoDatabase.Add(1)
		t.Skip("DATABASE_URL not set; run via `make test`")
	}
	return dsn
}

// TestMain makes a partial run of this package impossible to mistake for a
// full one: if any test skipped for lack of DATABASE_URL, print an unmissable
// warning naming the count and how to run the suite properly.
func TestMain(m *testing.M) {
	code := m.Run()
	if n := skippedForNoDatabase.Load(); n > 0 {
		bar := strings.Repeat("!", 78)
		fmt.Fprintf(os.Stderr, "\n%s\n%d test(s) in internal/legacy/store SKIPPED: DATABASE_URL was not set.\n"+
			"This is NOT a full run of this package's regression guards (bounty,\n"+
			"credit, calibration and anchor persistence). Run `make test` from the repo root instead.\n%s\n\n",
			bar, n, bar)
	}
	os.Exit(code)
}

func newTestDB(t *testing.T) *pgstore.DB {
	t.Helper()
	db, err := pgstore.Connect(context.Background(), testDSN(t))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), pactline.MigrationFS))
	t.Cleanup(db.Close)
	return db
}
