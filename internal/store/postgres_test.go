package store_test

import (
	"context"
	"os"
	"testing"

	"bountyboard"
	"bountyboard/internal/store"

	"github.com/stretchr/testify/require"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; run via `make test`")
	}
	return dsn
}

func TestConnectPings(t *testing.T) {
	db, err := store.Connect(context.Background(), testDSN(t))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Pool.Ping(context.Background()))
}

// TestMigrateIsSafeToCallRepeatedly proves that Migrate can be invoked more
// than once against the same database without failing and without
// duplicating schema_migrations rows. This guards against a regression to
// the non-atomic apply-then-record bug: since migrations/0001_init.sql uses
// bare CREATE TABLE (no IF NOT EXISTS), any re-execution of an
// already-applied migration body would fail with "relation already exists".
func TestMigrateIsSafeToCallRepeatedly(t *testing.T) {
	ctx := context.Background()
	db, err := store.Connect(ctx, testDSN(t))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Migrate(ctx, bountyboard.MigrationFS))
	require.NoError(t, db.Migrate(ctx, bountyboard.MigrationFS))

	rows, err := db.Pool.Query(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())

	seen := make(map[string]int, len(names))
	for _, name := range names {
		seen[name]++
	}
	for name, count := range seen {
		require.Equalf(t, 1, count, "schema_migrations has %d rows for %s, want exactly 1", count, name)
	}
	require.NotEmpty(t, names, "expected at least one recorded migration")
}
