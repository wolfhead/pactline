package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepositoryConnectionStoreCreateRotateAndDisable(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	connections := store.NewRepositoryConnectionStore(db)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connection := testRepositoryConnection(now)
	cleanupRepositoryConnection(t, db, connection.ID)

	created, err := connections.Create(
		ctx, connection, domain.SessionOperation(userA, "create-connection"),
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)
	require.Equal(t, connection.CredentialCiphertext, created.CredentialCiphertext)

	found, err := connections.FindActiveByRepository(ctx, domain.RepositoryReference{
		Provider: connection.Provider, Origin: connection.Origin, PathLookupKey: connection.PathLookupKey,
	})
	require.NoError(t, err)
	require.Equal(t, connection.ID, found.ID)

	rotated, err := connections.RotateCredential(
		ctx, connection.ID, 1,
		store.RepositoryConnectionValidation{
			Reference: domain.RepositoryReference{
				Provider: connection.Provider, Origin: connection.Origin, PathWithNamespace: "group/repo",
				PathLookupKey: "group/repo", WebURL: "https://gitlab.example/group/repo",
			},
			Repository: domain.RepositoryIdentity{
				ProviderRepositoryID: "17", PathWithNamespace: "group/repo",
				WebURL: "https://gitlab.example/group/repo", DefaultBranch: "trunk",
			},
			At: now.Add(time.Hour),
		},
		[]byte("replacement-ciphertext"), "key-2", nil,
		domain.SessionOperation(userA, "rotate-connection"),
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), rotated.Version)
	require.Equal(t, "trunk", rotated.DefaultBranch)
	require.Equal(t, "key-2", rotated.EncryptionKeyID)

	_, err = connections.Disable(
		ctx, connection.ID, 1, domain.SessionOperation(userA, "stale-disable"), now.Add(2*time.Hour),
	)
	var conflict domain.VersionConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, int64(2), conflict.CurrentVersion)

	disabled, err := connections.Disable(
		ctx, connection.ID, 2, domain.SessionOperation(userA, "disable-connection"), now.Add(2*time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, domain.RepositoryConnectionStatusDisabled, disabled.Status)
	require.Equal(t, int64(3), disabled.Version)
	require.NotNil(t, disabled.DisabledAt)

	_, err = connections.FindActiveByRepository(ctx, domain.RepositoryReference{
		Provider: connection.Provider, Origin: connection.Origin, PathLookupKey: connection.PathLookupKey,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)

	var eventCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM repository_connection_events WHERE connection_id=$1`, connection.ID,
	).Scan(&eventCount))
	require.Equal(t, 3, eventCount)
}

func TestRepositoryConnectionStoreRejectsDuplicateActiveRepository(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	connections := store.NewRepositoryConnectionStore(db)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	first := testRepositoryConnection(now)
	cleanupRepositoryConnection(t, db, first.ID)
	_, err := connections.Create(ctx, first, domain.SessionOperation(userA, "create-first"))
	require.NoError(t, err)

	second := testRepositoryConnection(now.Add(time.Minute))
	second.ID = uuid.New()
	cleanupRepositoryConnection(t, db, second.ID)
	_, err = connections.Create(ctx, second, domain.SessionOperation(userA, "create-second"))
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestProjectRepositoryBindingProtectsActiveConnectionAndVersionsProject(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connections := store.NewRepositoryConnectionStore(db)
	connection := testRepositoryConnection(now)
	cleanupRepositoryConnection(t, db, connection.ID)
	_, err := connections.Create(ctx, connection, domain.SessionOperation(userA, "create-binding-connection"))
	require.NoError(t, err)

	project, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Repository binding project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	repositories := store.NewProjectRepositoryStore(db)

	bound, err := repositories.Bind(
		ctx, project.Project.ID, project.Project.Version, connection.ID,
		domain.SessionOperation(userA, "bind-repository"), now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, project.Project.Version+1, bound.ProjectVersion)
	require.True(t, bound.Repository.Repository.Active())

	listed, err := repositories.ListActive(ctx, project.Project.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, connection.CanonicalWebURL, listed[0].Connection.CanonicalWebURL)

	_, err = connections.Disable(
		ctx, connection.ID, 1, domain.SessionOperation(userA, "disable-bound-connection"), now.Add(2*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	unbound, err := repositories.Unbind(
		ctx, project.Project.ID, bound.ProjectVersion, bound.Repository.Repository.ID,
		domain.SessionOperation(userA, "unbind-repository"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, bound.ProjectVersion+1, unbound.ProjectVersion)
	require.False(t, unbound.Repository.Repository.Active())

	_, err = connections.Disable(
		ctx, connection.ID, 1, domain.SessionOperation(userA, "disable-unbound-connection"), now.Add(4*time.Minute),
	)
	require.NoError(t, err)
}

func testRepositoryConnection(now time.Time) domain.RepositoryConnection {
	return domain.RepositoryConnection{
		ID: uuid.New(), Version: 1, Label: "Example repository",
		Provider: domain.RepositoryProviderGitLab,
		Origin:   "https://gitlab.example", ProviderRepositoryID: "17",
		PathWithNamespace: "group/repo", PathLookupKey: "group/repo",
		CanonicalWebURL: "https://gitlab.example/group/repo", DefaultBranch: "main",
		CredentialCiphertext: []byte("encrypted-credential"), EncryptionKeyID: "key-1",
		Status: domain.RepositoryConnectionStatusActive, LastValidatedAt: now,
		CreatedBy: userA, CreatedAt: now, UpdatedAt: now,
	}
}

func cleanupRepositoryConnection(t *testing.T, db *store.DB, connectionID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `
			DELETE FROM business_audit_events
			WHERE entity_type='repository_connection' AND entity_id=$1`, connectionID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connection_events WHERE connection_id=$1`, connectionID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM repository_connections WHERE id=$1`, connectionID)
		require.NoError(t, err)
	})
}
