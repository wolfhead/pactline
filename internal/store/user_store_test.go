package store_test

import (
	"context"
	"testing"

	"bountyboard"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Connect(context.Background(), testDSN(t))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), bountyboard.MigrationFS))
	t.Cleanup(db.Close)
	return db
}

func TestListActiveReturnsSeededUsers(t *testing.T) {
	db := newTestDB(t)
	users, err := store.NewUserStore(db).ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 6)
}

func TestGetByIDReturnsRoles(t *testing.T) {
	db := newTestDB(t)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	u, err := store.NewUserStore(db).GetByID(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "技术 Leader B", u.Name)
	require.True(t, u.HasRole(domain.UserRoleTechLead))
	require.False(t, u.HasRole(domain.UserRoleEngineer))
}
