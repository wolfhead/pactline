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

// TestListActiveReturnsSeededUsers pins both the `WHERE active` clause and the
// `ORDER BY name` clause of ListActive/ListAll, neither of which a bare
// Len(..., 6) check (with all six seeds active) can exercise: every seed
// starts active, so deleting the WHERE clause entirely would still return 6,
// and dropping ORDER BY would still return the right rows in a different
// order. 研发 E is deactivated for the duration of this test (and reactivated
// in cleanup, since every other test in this shared-database suite assumes
// all six seeded users are active) so ListActive must exclude it while
// ListAll still names it — and the expected order below is asserted
// exactly, not just its length.
func TestListActiveReturnsSeededUsers(t *testing.T) {
	db := newTestDB(t)
	us := store.NewUserStore(db)
	ctx := context.Background()

	deactivated := uuid.MustParse("00000000-0000-0000-0000-000000000005") // 研发 E
	require.NoError(t, us.SetActive(ctx, deactivated, false))
	t.Cleanup(func() {
		require.NoError(t, us.SetActive(context.Background(), deactivated, true))
	})

	active, err := us.ListActive(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"Steward F", "产品 A", "技术 Leader B", "研发 C", "研发 D"}, userNames(active),
		"ListActive must exclude the deactivated user and keep ORDER BY name")

	all, err := us.ListAll(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"Steward F", "产品 A", "技术 Leader B", "研发 C", "研发 D", "研发 E"}, userNames(all),
		"ListAll must still name a deactivated user")
}

func userNames(users []domain.User) []string {
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.Name
	}
	return names
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

// TestUserGetByIDMissingReturnsNotFound pins the ErrNotFound path: it is
// load-bearing for withIdentity's 401-versus-500 split (an unknown identity
// must be 401, not a 500 from an unexpected error), and that split was itself
// a bug fix.
func TestUserGetByIDMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := store.NewUserStore(db).GetByID(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}
