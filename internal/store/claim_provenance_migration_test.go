package store_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/wolfhead/pactline"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

func migrationsBeforeClaimProvenance(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	for _, name := range embeddedMigrationNames(t) {
		if name >= "0032_" {
			continue
		}
		body, err := fs.ReadFile(pactline.MigrationFS, "migrations/"+name)
		require.NoError(t, err)
		out["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	return out
}

func TestClaimProvenanceMigrationAppliesAfterMigration0031(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(ctx, migrationsBeforeClaimProvenance(t)))
	require.NoError(t, db.Migrate(ctx, pactline.MigrationFS))

	var migrationCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM schema_migrations
		WHERE name='0032_claim_client_provenance.sql'`).Scan(&migrationCount))
	require.Equal(t, 1, migrationCount)

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO api_request_audit_events (
			id,occurred_at,request_id,auth_method,auth_outcome,method,route_pattern,status_code,
			duration_ms,response_bytes,user_agent,client_kind,client_session_id
		) VALUES ('00000000-0000-0000-0000-000000000099',now(),
			'migration-provenance','session','authenticated','POST','/test',200,
			0,0,'test','pactline-cli','session-a')`)
	require.NoError(t, err)
}
