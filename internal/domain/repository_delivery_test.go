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

func TestCodeChangeSnapshotAllowsCoreIdentityWithoutProviderEvidence(t *testing.T) {
	now := time.Now().UTC()
	snapshot := domain.CodeChangeSnapshot{
		TaskCodeChangeID: uuid.New(), ProjectRepositoryID: uuid.New(),
		Provider: domain.RepositoryProviderGitLab,
		Kind:     domain.CodeChangeKindMergeRequest, ChangeNumber: 42,
		WebURL: "https://gitlab.example/group/repo/-/merge_requests/42",
	}
	require.NoError(t, snapshot.Validate())

	snapshot.ProviderEvidence = &domain.CodeChangeProviderEvidence{
		ConnectionID: uuid.New(), ProviderRepositoryID: "17", ProviderChangeID: "91",
		Title: "Implement evidence", State: domain.CodeChangeStateOpened,
		SourceBranch: "feature", TargetBranch: "main", HeadSHA: "abc123",
		ProviderUpdatedAt: now, ObservedAt: now,
	}
	require.NoError(t, snapshot.Validate())

	snapshot.ProviderEvidence.ProviderChangeID = ""
	require.Error(t, snapshot.Validate())
}

func TestTaskCodeChangeAllowsFailedVerificationWithoutProviderEvidence(t *testing.T) {
	now := time.Now().UTC()
	userID := uuid.New()
	change := domain.TaskCodeChange{
		ID: uuid.New(), TaskID: uuid.New(), ProjectID: uuid.New(), ProjectRepositoryID: uuid.New(),
		Provider: domain.RepositoryProviderGitHub, Kind: domain.CodeChangeKindPullRequest,
		ChangeNumber: 7, WebURL: "https://github.com/example/repo/pull/7",
		LinkedBy:             domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		LinkedThroughClaimID: uuid.New(), LinkedAt: now,
		ProviderVerification: &domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationUnreachable, AttemptedAt: now,
		},
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, change.Validate())
	change.ProviderVerification.Status = domain.CodeChangeVerificationVerified
	require.Error(t, change.Validate())
}
