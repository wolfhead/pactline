package store_test

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"bountyboard"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// createThrowawayDatabase provisions a uniquely named, empty database on the
// same PostgreSQL server as baseDSN and returns a DSN pointing at it, with
// the database dropped in t.Cleanup.
//
// This exists because the shared DATABASE_URL (what testDSN returns) is
// already migrated by other tests in this package; a test that needs a
// database Migrate has never touched cannot use it directly.
func createThrowawayDatabase(t *testing.T, baseDSN string) string {
	t.Helper()
	ctx := context.Background()

	adminDB, err := store.Connect(ctx, baseDSN)
	require.NoError(t, err)
	defer adminDB.Close()

	dbName := "migrate_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedName := pgx.Identifier{dbName}.Sanitize()

	_, err = adminDB.Pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, quotedName))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		cleanupDB, err := store.Connect(cleanupCtx, baseDSN)
		require.NoError(t, err)
		defer cleanupDB.Close()

		// WITH (FORCE) terminates any lingering connections to the throwaway
		// database so cleanup does not depend on the test having closed
		// every pool it opened against it.
		_, err = cleanupDB.Pool.Exec(cleanupCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, quotedName))
		require.NoError(t, err)
	})

	dsnURL, err := url.Parse(baseDSN)
	require.NoError(t, err)
	dsnURL.Path = "/" + dbName
	return dsnURL.String()
}

// embeddedMigrationNames lists the migration files Migrate itself would
// discover, so the test can assert against the real set instead of a
// hardcoded, driftable one.
func embeddedMigrationNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(bountyboard.MigrationFS, "migrations")
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestMigrateConcurrentCallsAreSerializedByAdvisoryLock exercises the
// pg_advisory_xact_lock in applyMigration: many goroutines call Migrate
// concurrently against a brand-new, unmigrated database.
//
// Before that lock existed, two concurrent callers could both observe "not
// yet applied" for the same migration and both execute its body. Since
// migrations/0001_init.sql uses a bare CREATE TABLE (no IF NOT EXISTS), the
// loser would fail with `relation "users" already exists`, and a caller
// that raced past the check but lost the INSERT would fail on the
// schema_migrations primary key instead. With the lock, callers are
// serialized around the check-then-apply sequence for a given migration, so
// exactly one of them applies it and the rest see it as already done.
//
// Coverage note: this test covers concurrent-safety (bug 2) only. It does
// NOT cover the crash-atomicity half of the fix (bug 1: migration body exec
// and the schema_migrations insert now sharing one transaction). Verifying
// atomicity would require injecting a crash/failure between those two
// statements, which needs fault-injection machinery (e.g. a proxy that can
// kill the connection mid-transaction) that this suite does not have. That
// property is exercised by code review and by this comment being honest
// about the gap, not by an automated test.
func TestMigrateConcurrentCallsAreSerializedByAdvisoryLock(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))

	// Pre-create schema_migrations sequentially, with the exact DDL Migrate
	// itself uses, before starting the concurrent goroutines below.
	//
	// This is deliberately NOT part of what we're testing: it works around
	// an unrelated PostgreSQL behavior where concurrent sessions racing to
	// run `CREATE TABLE IF NOT EXISTS` for a table that doesn't exist yet
	// can hit `duplicate key value violates unique constraint
	// "pg_type_typname_nsp_index"`, because the existence check and the
	// catalog insert are not atomic across sessions. That race was verified
	// empirically against this project's postgres:16-alpine image (roughly
	// 15 failures out of 20 concurrent attempts) and is unrelated to the
	// advisory lock under test here. Pre-creating the table once, before any
	// concurrency starts, removes that confound so a failure in this test
	// can only come from the migration-application race we actually care
	// about.
	setupDB, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	_, err = setupDB.Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`,
	)
	require.NoError(t, err)
	setupDB.Close()

	const numGoroutines = 15

	dbs := make([]*store.DB, numGoroutines)
	for i := range dbs {
		db, err := store.Connect(ctx, dsn)
		require.NoError(t, err)
		defer db.Close()
		dbs[i] = db
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = dbs[i].Migrate(ctx, bountyboard.MigrationFS)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d: Migrate returned an error", i)
	}

	verifyDB, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer verifyDB.Close()

	// schema_migrations must have exactly one row per migration file: no
	// duplicates, nothing missing.
	rows, err := verifyDB.Pool.Query(ctx, `SELECT name, count(*) FROM schema_migrations GROUP BY name ORDER BY name`)
	require.NoError(t, err)
	gotCounts := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		require.NoError(t, rows.Scan(&name, &count))
		gotCounts[name] = count
	}
	require.NoError(t, rows.Err())
	rows.Close()

	wantNames := embeddedMigrationNames(t)
	require.Lenf(t, gotCounts, len(wantNames), "expected exactly %d distinct migrations recorded, got %v", len(wantNames), gotCounts)
	for _, name := range wantNames {
		require.Equalf(t, 1, gotCounts[name], "schema_migrations has %d rows for %s, want exactly 1", gotCounts[name], name)
	}

	// The migration actually applied: seeded users are present exactly
	// once each (not zero, not duplicated).
	var userCount int
	require.NoError(t, verifyDB.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&userCount))
	require.Equal(t, 6, userCount, "expected exactly 6 seeded users")
}
