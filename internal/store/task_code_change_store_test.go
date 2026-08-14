package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

func TestTaskCodeChangeLinkAndUnlinkPreserveExecutionClaim(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connection := testRepositoryConnection(now)

	project, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Task delivery project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	repositories := store.NewProjectRepositoryStore(db)
	bound, err := repositories.Bind(
		ctx, project.Project.ID, project.Project.Version, repositoryReference(connection),
		domain.SessionOperation(userA, "bind-delivery-repository"), now.Add(time.Minute),
	)
	require.NoError(t, err)

	task := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Link multiple code changes", CreatorID: userA, ProjectID: project.Project.ID,
	}, nil)
	workflow := store.NewTaskWorkflowStore(db)
	ready, err := workflow.MarkReady(
		ctx, task.Task.Number, task.Task.Version,
		domain.SessionOperation(userA, "mark-delivery-ready"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, task.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "claim-delivery"), "browser", "", now.Add(3*time.Minute),
	)
	require.NoError(t, err)

	codeChanges := store.NewTaskCodeChangeStore(db)
	linked, err := codeChanges.Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.ID, testCodeChangeReference(42), actor,
		domain.SessionOperation(userA, "link-code-change"), now.Add(4*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, working.Version+1, linked.Task.Version)
	require.Equal(t, claim.Version, int64(1), "fixture Claim version must remain stable")
	require.Equal(t, domain.TaskPhaseInProgress, linked.Task.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityWorking, linked.Task.Lifecycle.Activity)
	_, err = codeChanges.Link(
		ctx, task.Task.Number, claim.ID, linked.Task.Version, claim.Version,
		bound.Repository.ID, testCodeChangeReference(42), actor,
		domain.SessionOperation(userA, "link-duplicate-code-change"), now.Add(4*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	activeClaim, err := store.NewTaskStageClaimStore(db).GetActiveForTaskNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.Equal(t, claim.ID, activeClaim.ID)
	require.Equal(t, claim.Version, activeClaim.Version)

	listed, err := codeChanges.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, int64(42), listed[0].CodeChange.ChangeNumber)

	unlinked, err := codeChanges.Unlink(
		ctx, task.Task.Number, claim.ID, linked.Task.Version, claim.Version,
		linked.CodeChange.CodeChange.ID, actor,
		domain.SessionOperation(userA, "unlink-code-change"), now.Add(5*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, linked.Task.Version+1, unlinked.Task.Version)
	require.False(t, unlinked.CodeChange.CodeChange.Active())

	listed, err = codeChanges.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Empty(t, listed)
}

func TestTaskCodeChangeRejectsWrongProjectBinding(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connection := testRepositoryConnection(now)
	first, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Delivery source project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, first.Project.ID)
	second, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Other delivery project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, second.Project.ID)
	bound, err := store.NewProjectRepositoryStore(db).Bind(
		ctx, second.Project.ID, second.Project.Version, repositoryReference(connection),
		domain.SessionOperation(userA, "bind-other-project"), now,
	)
	require.NoError(t, err)

	task := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Reject cross-project delivery", CreatorID: userA, ProjectID: first.Project.ID,
	}, nil)
	workflow := store.NewTaskWorkflowStore(db)
	ready, err := workflow.MarkReady(ctx, task.Task.Number, task.Task.Version,
		domain.SessionOperation(userA, "ready-cross-project"), now)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, task.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "claim-cross-project"), "browser", "", now,
	)
	require.NoError(t, err)

	_, err = store.NewTaskCodeChangeStore(db).Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.ID, testCodeChangeReference(42), actor,
		domain.SessionOperation(userA, "link-cross-project"), now,
	)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCompleteExecutionFreezesExactCodeChangeSetForReview(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	connection := testRepositoryConnection(now)
	cleanupRepositoryConnection(t, db, connection.ID)
	_, err := store.NewRepositoryConnectionStore(db).Create(
		ctx, connection, domain.SessionOperation(userA, "create-snapshot-connection"),
	)
	require.NoError(t, err)
	project, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Delivery snapshot project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	bound, err := store.NewProjectRepositoryStore(db).Bind(
		ctx, project.Project.ID, project.Project.Version, repositoryReference(connection),
		domain.SessionOperation(userA, "bind-snapshot-repository"), now,
	)
	require.NoError(t, err)
	task := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Freeze delivery evidence", CreatorID: userA, ProjectID: project.Project.ID,
	}, nil)
	workflow := store.NewTaskWorkflowStore(db)
	ready, err := workflow.MarkReady(ctx, task.Task.Number, task.Task.Version,
		domain.SessionOperation(userA, "ready-snapshot-task"), now)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, task.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "claim-snapshot-task"), "browser", "", now,
	)
	require.NoError(t, err)
	codeChanges := store.NewTaskCodeChangeStore(db)
	first, err := codeChanges.Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.ID, testCodeChangeReference(42), actor,
		domain.SessionOperation(userA, "link-snapshot-mr-1"), now,
	)
	require.NoError(t, err)
	firstEvidence := testCodeChangeEvidence(connection, "91", "Implement delivery", "abc123", now)
	require.NoError(t, codeChanges.UpdateProviderEvidence(ctx, first.CodeChange.CodeChange.ID,
		firstEvidence, verifiedAt(now), now))
	secondChange := testCodeChangeReference(43)
	second, err := codeChanges.Link(
		ctx, task.Task.Number, claim.ID, first.Task.Version, claim.Version,
		bound.Repository.ID, secondChange, actor,
		domain.SessionOperation(userA, "link-snapshot-mr-2"), now.Add(time.Minute),
	)
	require.NoError(t, err)
	secondEvidence := testCodeChangeEvidence(connection, "92", "Document delivery", "def456", now.Add(time.Minute))
	require.NoError(t, codeChanges.UpdateProviderEvidence(ctx, second.CodeChange.CodeChange.ID,
		secondEvidence, verifiedAt(now.Add(time.Minute)), now.Add(time.Minute)))

	_, _, _, err = workflow.CompleteExecution(
		ctx, task.Task.Number, claim.ID, second.Task.Version, claim.Version,
		"Missing prepared delivery", actor,
		domain.SessionOperation(userA, "complete-without-snapshot"), now.Add(2*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	links, err := codeChanges.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	snapshots := make([]domain.CodeChangeSnapshot, len(links))
	for index, link := range links {
		snapshots[index] = domain.CodeChangeSnapshot{
			TaskCodeChangeID: link.CodeChange.ID, ProjectRepositoryID: link.Repository.ID,
			Provider: link.CodeChange.Provider, Kind: link.CodeChange.Kind,
			ChangeNumber: link.CodeChange.ChangeNumber, WebURL: link.CodeChange.WebURL,
			ProviderEvidence: link.CodeChange.ProviderEvidence,
		}
	}
	review, completedClaim, completion, err := workflow.CompleteExecutionWithDelivery(
		ctx, task.Task.Number, claim.ID, second.Task.Version, claim.Version,
		"Ready for review", snapshots, actor,
		domain.SessionOperation(userA, "complete-with-snapshot"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseInReview, review.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityAvailable, review.Lifecycle.Activity)
	require.Equal(t, domain.StageClaimStatusCompleted, completedClaim.Status)
	require.NotNil(t, completion.ExecutionCompleted)
	require.Equal(t, snapshots, completion.ExecutionCompleted.CodeChanges)

	moved := *links[0].CodeChange.ProviderEvidence
	moved.HeadSHA = "new-head"
	moved.ObservedAt = now.Add(4 * time.Minute)
	require.NoError(t, codeChanges.UpdateProviderEvidence(
		ctx, links[0].CodeChange.ID, moved, verifiedAt(now.Add(4*time.Minute)), now.Add(4*time.Minute),
	))
	frozen, err := codeChanges.GetReviewSnapshot(ctx, task.Task.ID, review.Lifecycle.ReviewCycle)
	require.NoError(t, err)
	require.NotNil(t, frozen)
	expectedSnapshots := append([]domain.CodeChangeSnapshot(nil), snapshots...)
	for index := range expectedSnapshots {
		expectedSnapshots[index].ProviderEvidence.ObservedAt = expectedSnapshots[index].ProviderEvidence.ObservedAt.UTC()
		expectedSnapshots[index].ProviderEvidence.ProviderUpdatedAt = expectedSnapshots[index].ProviderEvidence.ProviderUpdatedAt.UTC()
	}
	require.Equal(t, expectedSnapshots, frozen.CodeChanges,
		"provider refresh must not mutate the frozen review snapshot")

	reviewWorking, reviewClaim, err := workflow.Claim(
		ctx, task.Task.Number, review.Version, actor,
		domain.SessionOperation(userA, "claim-review-task"), "browser", "", now.Add(5*time.Minute),
	)
	require.NoError(t, err)
	_, err = codeChanges.Link(
		ctx, task.Task.Number, reviewClaim.ID, reviewWorking.Version, reviewClaim.Version,
		bound.Repository.ID, secondChange, actor,
		domain.SessionOperation(userA, "link-during-review"), now.Add(6*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrInvalidTransition)
}

func testCodeChangeReference(number int64) domain.CodeChangeReference {
	connection := testRepositoryConnection(time.Now())
	return domain.CodeChangeReference{
		Repository: repositoryReference(connection), Kind: domain.CodeChangeKindMergeRequest,
		ChangeNumber: number,
		WebURL:       "https://gitlab.example/group/repo/-/merge_requests/" + fmt.Sprint(number),
	}
}

func testCodeChangeEvidence(
	connection domain.RepositoryConnection,
	providerChangeID string,
	title string,
	headSHA string,
	now time.Time,
) domain.CodeChangeProviderEvidence {
	return domain.CodeChangeProviderEvidence{
		ConnectionID: connection.ID, ProviderRepositoryID: connection.ProviderRepositoryID,
		ProviderChangeID: providerChangeID, Title: title, State: domain.CodeChangeStateOpened,
		SourceBranch: "feature", TargetBranch: "main", HeadSHA: headSHA,
		ProviderUpdatedAt: now, ObservedAt: now,
	}
}

func verifiedAt(now time.Time) domain.CodeChangeVerification {
	return domain.CodeChangeVerification{Status: domain.CodeChangeVerificationVerified, AttemptedAt: now}
}
