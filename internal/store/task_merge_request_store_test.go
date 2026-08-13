package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

func TestTaskMergeRequestLinkAndUnlinkPreserveExecutionClaim(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connections := store.NewGitLabConnectionStore(db)
	connection := testGitLabConnection(now)
	cleanupGitLabConnection(t, db, connection.ID)
	_, err := connections.Create(ctx, connection, domain.SessionOperation(userA, "create-delivery-connection"))
	require.NoError(t, err)

	project, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Task delivery project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	repositories := store.NewProjectRepositoryStore(db)
	bound, err := repositories.Bind(
		ctx, project.Project.ID, project.Project.Version, connection.ID,
		domain.SessionOperation(userA, "bind-delivery-repository"), now.Add(time.Minute),
	)
	require.NoError(t, err)

	task := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Link multiple merge requests", CreatorID: userA, ProjectID: project.Project.ID,
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

	mergeRequests := store.NewTaskMergeRequestStore(db)
	linked, err := mergeRequests.Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.Repository.ID, testGitLabMergeRequest(now.Add(4*time.Minute)), actor,
		domain.SessionOperation(userA, "link-mr"), now.Add(4*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, working.Version+1, linked.Task.Version)
	require.Equal(t, claim.Version, int64(1), "fixture Claim version must remain stable")
	require.Equal(t, domain.TaskPhaseInProgress, linked.Task.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityWorking, linked.Task.Lifecycle.Activity)

	activeClaim, err := store.NewTaskStageClaimStore(db).GetActiveForTaskNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.Equal(t, claim.ID, activeClaim.ID)
	require.Equal(t, claim.Version, activeClaim.Version)

	listed, err := mergeRequests.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, int64(42), listed[0].MergeRequest.MergeRequestIID)

	unlinked, err := mergeRequests.Unlink(
		ctx, task.Task.Number, claim.ID, linked.Task.Version, claim.Version,
		linked.MergeRequest.MergeRequest.ID, actor,
		domain.SessionOperation(userA, "unlink-mr"), now.Add(5*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, linked.Task.Version+1, unlinked.Task.Version)
	require.False(t, unlinked.MergeRequest.MergeRequest.Active())

	listed, err = mergeRequests.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Empty(t, listed)
}

func TestTaskMergeRequestRejectsWrongProjectBinding(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	connection := testGitLabConnection(now)
	cleanupGitLabConnection(t, db, connection.ID)
	_, err := store.NewGitLabConnectionStore(db).Create(
		ctx, connection, domain.SessionOperation(userA, "create-cross-project-connection"),
	)
	require.NoError(t, err)
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
		ctx, second.Project.ID, second.Project.Version, connection.ID,
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

	_, err = store.NewTaskMergeRequestStore(db).Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.Repository.ID, testGitLabMergeRequest(now), actor,
		domain.SessionOperation(userA, "link-cross-project"), now,
	)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCompleteExecutionFreezesExactMergeRequestSetForReview(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	connection := testGitLabConnection(now)
	cleanupGitLabConnection(t, db, connection.ID)
	_, err := store.NewGitLabConnectionStore(db).Create(
		ctx, connection, domain.SessionOperation(userA, "create-snapshot-connection"),
	)
	require.NoError(t, err)
	project, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Delivery snapshot project", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	bound, err := store.NewProjectRepositoryStore(db).Bind(
		ctx, project.Project.ID, project.Project.Version, connection.ID,
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
	mergeRequests := store.NewTaskMergeRequestStore(db)
	first, err := mergeRequests.Link(
		ctx, task.Task.Number, claim.ID, working.Version, claim.Version,
		bound.Repository.Repository.ID, testGitLabMergeRequest(now), actor,
		domain.SessionOperation(userA, "link-snapshot-mr-1"), now,
	)
	require.NoError(t, err)
	secondMR := testGitLabMergeRequest(now.Add(time.Minute))
	secondMR.ID, secondMR.IID = 92, 43
	secondMR.WebURL = "https://gitlab.example/group/repo/-/merge_requests/43"
	secondMR.Observation.Title = "Document delivery"
	secondMR.Observation.HeadSHA = "def456"
	second, err := mergeRequests.Link(
		ctx, task.Task.Number, claim.ID, first.Task.Version, claim.Version,
		bound.Repository.Repository.ID, secondMR, actor,
		domain.SessionOperation(userA, "link-snapshot-mr-2"), now.Add(time.Minute),
	)
	require.NoError(t, err)

	_, _, _, err = workflow.CompleteExecution(
		ctx, task.Task.Number, claim.ID, second.Task.Version, claim.Version,
		"Missing prepared delivery", actor,
		domain.SessionOperation(userA, "complete-without-snapshot"), now.Add(2*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	links, err := mergeRequests.ListActive(ctx, task.Task.ID)
	require.NoError(t, err)
	snapshots := make([]domain.MergeRequestSnapshot, len(links))
	for index, link := range links {
		observation := link.MergeRequest.LatestObservation
		snapshots[index] = domain.MergeRequestSnapshot{
			TaskMergeRequestID:  link.MergeRequest.ID,
			ProjectRepositoryID: link.Repository.ID,
			ConnectionID:        link.Connection.ID,
			GitLabProjectID:     link.Connection.GitLabProjectID,
			MergeRequestIID:     link.MergeRequest.MergeRequestIID,
			WebURL:              link.MergeRequest.WebURL,
			Title:               observation.Title, State: observation.State, Draft: observation.Draft,
			SourceBranch: observation.SourceBranch, TargetBranch: observation.TargetBranch,
			HeadSHA: observation.HeadSHA, MergeCommitSHA: observation.MergeCommitSHA,
			MergedAt: observation.MergedAt, ObservationStatus: observation.Status,
			ObservedAt: observation.ObservedAt,
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
	require.Equal(t, snapshots, completion.ExecutionCompleted.MergeRequests)

	moved := links[0].MergeRequest.LatestObservation
	moved.HeadSHA = "new-head"
	moved.ObservedAt = now.Add(4 * time.Minute)
	require.NoError(t, mergeRequests.UpdateObservation(
		ctx, links[0].MergeRequest.ID, moved, now.Add(4*time.Minute),
	))
	frozen, err := mergeRequests.GetReviewSnapshot(ctx, task.Task.ID, review.Lifecycle.ReviewCycle)
	require.NoError(t, err)
	require.NotNil(t, frozen)
	require.Equal(t, snapshots, frozen.MergeRequests, "provider refresh must not mutate the frozen review snapshot")

	reviewWorking, reviewClaim, err := workflow.Claim(
		ctx, task.Task.Number, review.Version, actor,
		domain.SessionOperation(userA, "claim-review-task"), "browser", "", now.Add(5*time.Minute),
	)
	require.NoError(t, err)
	_, err = mergeRequests.Link(
		ctx, task.Task.Number, reviewClaim.ID, reviewWorking.Version, reviewClaim.Version,
		bound.Repository.Repository.ID, secondMR, actor,
		domain.SessionOperation(userA, "link-during-review"), now.Add(6*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrInvalidTransition)
}

func testGitLabMergeRequest(now time.Time) domain.GitLabMergeRequest {
	return domain.GitLabMergeRequest{
		ID: 91, IID: 42,
		WebURL: "https://gitlab.example/group/repo/-/merge_requests/42",
		Observation: domain.GitLabMergeRequestObservation{
			Status: domain.GitLabObservationConfirmed, ObservedAt: now,
			Title: "Implement delivery", State: domain.GitLabMergeRequestOpened,
			SourceBranch: "feature", TargetBranch: "main", HeadSHA: "abc123",
			ProviderUpdatedAt: now,
		},
	}
}
