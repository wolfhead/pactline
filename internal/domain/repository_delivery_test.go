package domain_test

import (
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCodeChangeKindRequiresCompatibleProvider(t *testing.T) {
	require.True(t, domain.CodeChangeKindMergeRequest.CompatibleWith(domain.RepositoryProviderGitLab))
	require.True(t, domain.CodeChangeKindPullRequest.CompatibleWith(domain.RepositoryProviderGitHub))
	require.False(t, domain.CodeChangeKindPullRequest.CompatibleWith(domain.RepositoryProviderGitLab))
	require.False(t, domain.CodeChangeKindMergeRequest.CompatibleWith(domain.RepositoryProviderGitHub))
}

func TestCodeChangeObservationAllowsDegradedEvidenceWithoutProviderMetadata(t *testing.T) {
	require.NoError(t, (domain.CodeChangeObservation{
		Status: domain.CodeChangeObservationMissing, ObservedAt: time.Now().UTC(),
	}).Validate())
	require.Error(t, (domain.CodeChangeObservation{
		Status: domain.CodeChangeObservationConfirmed, ObservedAt: time.Now().UTC(),
	}).Validate())
}

func TestCodeChangeSnapshotRequiresStableProviderIdentities(t *testing.T) {
	now := time.Now().UTC()
	snapshot := domain.CodeChangeSnapshot{
		TaskCodeChangeID: uuid.New(), ProjectRepositoryID: uuid.New(), ConnectionID: uuid.New(),
		Provider: domain.RepositoryProviderGitLab, ProviderRepositoryID: "17",
		Kind: domain.CodeChangeKindMergeRequest, ChangeNumber: 42, ProviderChangeID: "91",
		WebURL: "https://gitlab.example/group/repo/-/merge_requests/42",
		Title:  "Implement evidence", State: domain.CodeChangeStateOpened, HeadSHA: "abc123",
		ObservationStatus: domain.CodeChangeObservationConfirmed, ObservedAt: now,
	}
	require.NoError(t, snapshot.Validate())

	snapshot.ProviderChangeID = ""
	require.Error(t, snapshot.Validate())

	snapshot.ProviderChangeID = "91"
	snapshot.Kind = domain.CodeChangeKindPullRequest
	require.Error(t, snapshot.Validate())
}
