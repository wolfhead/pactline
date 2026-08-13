package domain_test

import (
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestThreadRolesAndIssueTypesAreExplicit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	taskID := uuid.New()
	userID := uuid.New()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}

	main, err := domain.NewMainThread(taskID, now)
	require.NoError(t, err)
	require.Equal(t, domain.ThreadRoleMain, main.Role)
	require.Empty(t, main.IssueType)

	for _, issueType := range []domain.IssueThreadType{
		domain.IssueThreadTypeDecisionRequired,
		domain.IssueThreadTypeDependencyRequired,
	} {
		issue, err := domain.NewIssueThread(
			taskID, issueType, domain.TaskPhaseInProgress, actor, now,
		)
		require.NoError(t, err)
		require.Equal(t, issueType, issue.IssueType)
		require.Equal(t, domain.IssueThreadStatusOpen, issue.IssueStatus)
	}

	_, err = domain.NewIssueThread(
		taskID, domain.IssueThreadType("other"), domain.TaskPhaseInProgress, actor, now,
	)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = domain.NewIssueThread(
		taskID, domain.IssueThreadTypeDecisionRequired, domain.TaskPhaseReady, actor, now,
	)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestIssueThreadResolvesOnceWithoutChangingItsType(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	opener := domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex/session-a"}
	resolverID := uuid.New()
	resolver := domain.Actor{Type: domain.ActorTypeUser, UserID: &resolverID}
	issue, err := domain.NewIssueThread(
		uuid.New(), domain.IssueThreadTypeDecisionRequired,
		domain.TaskPhaseInReview, opener, now,
	)
	require.NoError(t, err)

	require.NoError(t, issue.Resolve(resolver, now.Add(time.Hour)))
	require.Equal(t, domain.IssueThreadTypeDecisionRequired, issue.IssueType)
	require.Equal(t, domain.IssueThreadStatusResolved, issue.IssueStatus)
	require.Equal(t, int64(2), issue.Version)
	require.ErrorIs(t, issue.Resolve(resolver, now.Add(2*time.Hour)), domain.ErrConflict)
}

func TestOnlyOrdinaryThreadMessagesAreMutable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	userID := uuid.New()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}

	message := domain.ThreadItem{
		ID: uuid.New(), ThreadID: uuid.New(), Kind: domain.ThreadItemKindMessage,
		Author: actor, Body: "Initial", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, message.Validate())
	require.NoError(t, message.Edit("Updated", []uuid.UUID{uuid.New()}, now.Add(time.Minute)))
	require.Equal(t, int64(2), message.Version)
	require.NoError(t, message.Delete(now.Add(2*time.Minute)))
	require.Empty(t, message.Body)

	request := domain.ThreadItem{
		ID: uuid.New(), ThreadID: uuid.New(), Kind: domain.ThreadItemKindResolutionRequest,
		Author: actor, Body: "Which behavior is required?", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, request.Validate())
	require.ErrorIs(t, request.Edit("Changed", nil, now.Add(time.Minute)), domain.ErrConflict)
	require.ErrorIs(t, request.Delete(now.Add(time.Minute)), domain.ErrConflict)
}

func TestIssueResolutionSummaryRequiresRequestAndConclusion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	userID := uuid.New()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}
	payload := domain.IssueResolutionPayload{
		IssueThreadID: uuid.New(), IssueType: domain.IssueThreadTypeDependencyRequired,
		Request:    "The test environment credential is missing.",
		Resolution: "A scoped test credential was provisioned.",
		OpenedBy:   actor, ResolvedBy: actor, OpenedAt: now, ResolvedAt: now.Add(time.Hour),
	}
	item := domain.ThreadItem{
		ID: uuid.New(), ThreadID: uuid.New(), Kind: domain.ThreadItemKindIssueResolution,
		Author: actor, IssueResolution: &payload, Version: 1,
		CreatedAt: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour),
	}
	require.NoError(t, item.Validate())

	item.IssueResolution.Resolution = ""
	require.ErrorIs(t, item.Validate(), domain.ErrInvalidInput)
}

func TestDeliveryThreadItemsRequireClaimAndReviewCycleContext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	claimID := uuid.New()
	cycle := int64(2)
	item := domain.ThreadItem{
		ID: uuid.New(), ThreadID: uuid.New(), Kind: domain.ThreadItemKindWorkSubmission,
		Author: domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex/session-a"},
		Body:   "Implemented and tested the requested change.", TaskStageClaimID: &claimID,
		TaskReviewCycle: &cycle, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, item.Validate())

	item.Kind = domain.ThreadItemKindExecutionCompleted
	item.ExecutionCompleted = &domain.ExecutionCompletedPayload{
		ReviewCycle: cycle, SubmissionItemIDs: []uuid.UUID{item.ID},
		CriterionRevisions: []domain.CriterionRevisionSnapshot{{CriterionID: uuid.New(), Revision: 1}},
	}
	require.NoError(t, item.Validate())

	item.ExecutionCompleted.ReviewCycle++
	require.ErrorIs(t, item.Validate(), domain.ErrInvalidInput)
}
