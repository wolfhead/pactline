package store_test

import (
	"context"
	"testing"
	"time"

	"bountyboard"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// TestListActiveReturnsSeededUsers pins both seed consolidation and the
// difference between active identity choices and historical user references.
func TestListActiveReturnsSeededUsers(t *testing.T) {
	db := newTestDB(t)
	us := store.NewUserStore(db)
	ctx := context.Background()

	active, err := us.ListActive(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"产品 A"}, userNames(active),
		"only the primary seed remains active after identity consolidation")

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
	require.False(t, u.Active)
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

func TestUserStoreRoundTripsIdentityProfile(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	id := uuid.New()
	avatarURL := "https://example.com/avatar.png"

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id, name, email, avatar_url, platform_role, roles, active)
		VALUES ($1, $2, NULL, $3, 'MEMBER', $4, true)
	`, id, "Nullable Email User", avatarURL, []string{"ENGINEER"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
		require.NoError(t, cleanupErr)
	})

	u, err := store.NewUserStore(db).GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, u.ID)
	require.Equal(t, "Nullable Email User", u.Name)
	require.Nil(t, u.Email)
	require.NotNil(t, u.AvatarURL)
	require.Equal(t, avatarURL, *u.AvatarURL)
	require.Equal(t, domain.PlatformRoleMember, u.PlatformRole)
	require.Equal(t, []domain.UserRole{domain.UserRoleEngineer}, u.Roles)
	require.True(t, u.Active)
	require.False(t, u.CreatedAt.IsZero())
	var databaseNow time.Time
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow))
	require.WithinDuration(t, databaseNow, u.UpdatedAt, time.Minute)
}

func TestUsersPermitOneAdminOnly(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	primaryID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	secondaryID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	_, err := db.Pool.Exec(ctx, `UPDATE users SET platform_role = 'ADMIN' WHERE id = $1`, primaryID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET platform_role = 'MEMBER' WHERE id = $1`, primaryID)
		require.NoError(t, cleanupErr)
	})

	_, err = db.Pool.Exec(ctx, `UPDATE users SET platform_role = 'ADMIN' WHERE id = $1`, secondaryID)
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code)
}
