package store_test

import (
	"context"
	"os"
	"testing"

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
