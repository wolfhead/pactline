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
	"sync/atomic"
	"testing"
	"testing/fstest"

	"bountyboard"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// skippedForNoDatabase counts tests in this package that skipped for lack of
// DATABASE_URL, so TestMain can print a warning no one can mistake for a
// clean, full run.
var skippedForNoDatabase atomic.Int64

// testDSN returns the shared test database DSN, or stops the calling test.
//
// A3/A5 concern: `go test ./...` run bare (no DATABASE_URL) still reports
// PASS if every store test just skips — the two mandated regression guards
// (migration integrity, active-user seeding) silently vanish. Under CI this
// must be a hard failure rather than a quiet skip, since a green CI run is
// exactly the signal a maintainer trusts without looking closer. Locally,
// where a bare `go test ./...` during development is normal, skip but count
// it so TestMain can shout about the shortfall on exit.
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
		fmt.Fprintf(os.Stderr, "\n%s\n%d test(s) in internal/store SKIPPED: DATABASE_URL was not set.\n"+
			"This is NOT a full run of this package's regression guards (migrations,\n"+
			"active-user seeding). Run `make test` from the repo root instead.\n%s\n\n",
			bar, n, bar)
	}
	os.Exit(code)
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

func migrationsBeforeIdentity(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	for _, name := range embeddedMigrationNames(t) {
		if name >= "0009_" {
			continue
		}
		body, err := fs.ReadFile(bountyboard.MigrationFS, "migrations/"+name)
		require.NoError(t, err)
		out["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	return out
}

func TestIdentityMigrationConsolidatesSeedAttribution(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Migrate(ctx, migrationsBeforeIdentity(t)))

	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[i] = uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1))
	}
	bountyID := uuid.New()
	secondBountyID := uuid.New()
	calibrationID := uuid.New()
	projectID := uuid.New()
	milestoneID := uuid.New()
	taskID := uuid.New()
	criterionID := uuid.New()
	unrelatedUserID := uuid.New()
	unrelatedCreditID := uuid.MustParse("10000000-0000-0000-0000-000000000005")

	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, name, email)
		VALUES ($1, 'Unrelated real user', 'unrelated@example.com')
	`, unrelatedUserID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO bounties
			(id, type, title, directed_to, sponsor_id, claimed_by)
		VALUES
			($1, 'TASK', 'Credit status priority', $3, $4, $5),
			($2, 'TASK', 'Credit timestamp priority', $3, $4, $5)
	`, bountyID, secondBountyID, ids[1], ids[2], ids[3])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO credits
			(id, bounty_id, user_id, role, nominated_by, status, confirmed_at, created_at)
		VALUES
			('10000000-0000-0000-0000-000000000001', $1, $3, 'IMPLEMENTER', $5, 'PENDING', NULL, '2026-01-01T00:00:00Z'),
			('10000000-0000-0000-0000-000000000002', $1, $4, 'IMPLEMENTER', $5, 'CONFIRMED', '2026-02-01T00:00:00Z', '2026-02-01T00:00:00Z'),
			('10000000-0000-0000-0000-000000000003', $2, $3, 'REVIEWER', $5, 'CONFIRMED', '2026-03-01T00:00:00Z', '2026-01-01T00:00:00Z'),
			('10000000-0000-0000-0000-000000000004', $2, $4, 'REVIEWER', $5, 'CONFIRMED', '2026-02-01T00:00:00Z', '2026-04-01T00:00:00Z'),
			($6, $1, $7, 'IMPLEMENTER', $5, 'PENDING', NULL, '2026-05-01T00:00:00Z')
	`, bountyID, secondBountyID, ids[1], ids[2], ids[5], unrelatedCreditID, unrelatedUserID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO calibrations
			(id, bounty_id, quarter, original_value, calibrated_value,
			 calibrated_score, created_by, original_score)
		VALUES ($1, $2, '2026-Q1', 'L1', 'L2', 2, $3, 1)
	`, calibrationID, bountyID, ids[1])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO projects
			(id, name, outcome, owner_id, creator_id)
		VALUES ($1, 'Identity project', 'Consolidated attribution', $2, $3)
	`, projectID, ids[1], ids[2])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO milestones
			(id, project_id, name, outcome, position)
		VALUES ($1, $2, 'Identity milestone', 'Migration complete', 0)
	`, milestoneID, projectID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO tasks
			(id, title, assignee_id, creator_id, project_id, milestone_id)
		VALUES ($1, 'Identity task', $2, $3, $4, $5)
	`, taskID, ids[3], ids[5], projectID, milestoneID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO task_comments (id, task_id, author_id, body)
		VALUES (gen_random_uuid(), $1, $2, 'Historical comment')
	`, taskID, ids[5])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO task_activity (id, task_id, actor_id, field)
		VALUES (gen_random_uuid(), $1, $2, 'created')
	`, taskID, ids[1])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO acceptance_criteria
			(id, task_id, criterion, verification_instructions, position)
		VALUES ($1, $2, 'Migration passes', 'Run the test', 0)
	`, criterionID, taskID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO acceptance_checks
			(id, criterion_id, criterion_revision, outcome, evidence,
			 checker_type, checked_by_user_id)
		VALUES (gen_random_uuid(), $1, 1, 'passed', 'Test result', 'user', $2)
	`, criterionID, ids[2])
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO project_activity
			(id, project_id, milestone_id, actor_id, action)
		VALUES (gen_random_uuid(), $1, $2, $3, 'created')
	`, projectID, milestoneID, ids[3])
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, db.Migrate(ctx, bountyboard.MigrationFS))

	var activeSecondary int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM users
		WHERE id = ANY($1) AND active
	`, ids[1:]).Scan(&activeSecondary))
	require.Zero(t, activeSecondary)

	var incorrectlyAttributed int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT directed_to AS user_id FROM bounties WHERE directed_to IS NOT NULL
			UNION ALL SELECT sponsor_id FROM bounties
			UNION ALL SELECT claimed_by FROM bounties WHERE claimed_by IS NOT NULL
			UNION ALL SELECT user_id FROM credits
			UNION ALL SELECT nominated_by FROM credits WHERE nominated_by IS NOT NULL
			UNION ALL SELECT created_by FROM calibrations
			UNION ALL SELECT assignee_id FROM tasks WHERE assignee_id IS NOT NULL
			UNION ALL SELECT creator_id FROM tasks
			UNION ALL SELECT author_id FROM task_comments
			UNION ALL SELECT actor_id FROM task_activity
			UNION ALL SELECT owner_id FROM projects
			UNION ALL SELECT creator_id FROM projects
			UNION ALL SELECT checked_by_user_id FROM acceptance_checks WHERE checked_by_user_id IS NOT NULL
			UNION ALL SELECT actor_id FROM project_activity
		) attributed
		WHERE user_id = ANY($1)
	`, ids[1:]).Scan(&incorrectlyAttributed))
	require.Zero(t, incorrectlyAttributed)

	rows, err := db.Pool.Query(ctx, `
		SELECT bounty_id, role, id
		FROM credits
		WHERE user_id = $1
		ORDER BY bounty_id, role
	`, ids[0])
	require.NoError(t, err)
	defer rows.Close()
	kept := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var gotBountyID uuid.UUID
		var role string
		var creditID uuid.UUID
		require.NoError(t, rows.Scan(&gotBountyID, &role, &creditID))
		kept[gotBountyID] = creditID
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[uuid.UUID]uuid.UUID{
		bountyID:       uuid.MustParse("10000000-0000-0000-0000-000000000002"),
		secondBountyID: uuid.MustParse("10000000-0000-0000-0000-000000000004"),
	}, kept)

	var unrelatedCreditUserID uuid.UUID
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT user_id FROM credits WHERE id = $1`, unrelatedCreditID,
	).Scan(&unrelatedCreditUserID))
	require.Equal(t, unrelatedUserID, unrelatedCreditUserID,
		"a credit that does not collide with the consolidated seed user must survive")

	var userCount, adminCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&userCount))
	require.Equal(t, 7, userCount, "the migration must not hard-delete users")
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE platform_role = 'ADMIN'`,
	).Scan(&adminCount))
	require.Zero(t, adminCount, "administrator assignment belongs to bootstrap, not migration")
}

func TestIdentityMigrationCreatesAccessSchema(t *testing.T) {
	db, err := store.Connect(context.Background(), testDSN(t))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(context.Background(), bountyboard.MigrationFS))

	expectedTables := []string{
		"external_identities",
		"oauth_credentials",
		"invitations",
		"invitation_deliveries",
		"sessions",
		"authorization_transactions",
		"impersonations",
		"identity_audit_events",
	}
	for _, table := range expectedTables {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(context.Background(),
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists))
		require.Truef(t, exists, "%s should exist", table)
	}
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
