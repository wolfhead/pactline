package store_test

import (
	"context"
	"testing"

	"github.com/wolfhead/pactline"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

func TestAgentAPIFoundationSchema(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Migrate(ctx, pactline.MigrationFS))

	for _, table := range []string{
		"api_tokens",
		"api_request_audit_events",
		"business_audit_events",
		"idempotency_records",
	} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx, `
			SELECT to_regclass('public.' || $1) IS NOT NULL
		`, table).Scan(&exists))
		require.True(t, exists, "%s must exist", table)
	}

	for _, table := range []string{
		"tasks",
		"task_comments",
		"projects",
		"milestones",
		"acceptance_criteria",
		"labels",
	} {
		var dataType string
		var nullable bool
		var defaultValue string
		require.NoError(t, db.Pool.QueryRow(ctx, `
			SELECT data_type, is_nullable = 'YES', column_default
			FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name='version'
		`, table).Scan(&dataType, &nullable, &defaultValue))
		require.Equal(t, "bigint", dataType, table)
		require.False(t, nullable, table)
		require.Equal(t, "1", defaultValue, table)
	}

	var secretHashUnique bool
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index i
			JOIN pg_attribute a
			  ON a.attrelid=i.indrelid AND a.attnum=ANY(i.indkey)
			WHERE i.indrelid='api_tokens'::regclass
			  AND i.indisunique
			  AND a.attname='secret_hash'
		)
	`).Scan(&secretHashUnique))
	require.True(t, secretHashUnique)

	rows, err := db.Pool.Query(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema='public'
		  AND table_name IN (
		    'api_tokens','api_request_audit_events',
		    'business_audit_events','idempotency_records'
		  )
		  AND column_name IN ('token','secret','authorization_header')
	`)
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next(), "raw credential columns are forbidden")
	require.NoError(t, rows.Err())
}
