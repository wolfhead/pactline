package store_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wolfhead/pactline"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func migrationsBeforeProviderNeutralRepositoryDelivery(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	for _, name := range embeddedMigrationNames(t) {
		if name >= "0033_" {
			continue
		}
		body, err := fs.ReadFile(pactline.MigrationFS, "migrations/"+name)
		require.NoError(t, err)
		out["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	return out
}

func TestProviderNeutralRepositoryDeliveryMigrationPreservesGitLabData(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Migrate(ctx, migrationsBeforeProviderNeutralRepositoryDelivery(t)))

	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	projectResult, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Repository migration fixture", CreatorID: userA,
	})
	require.NoError(t, err)
	taskResult, err := store.NewTaskStore(db).Create(ctx, domain.Task{
		Title: "Preserve delivery", Context: "Migration fixture",
		ExpectedResult: "Delivery remains reviewable", CreatorID: userA,
		ProjectID: projectResult.Project.ID,
	}, nil)
	require.NoError(t, err)

	connectionID := uuid.New()
	bindingID := uuid.New()
	claimID := uuid.New()
	activeLinkID := uuid.New()
	unlinkedLinkID := uuid.New()
	eventID := uuid.New()
	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	err = func() error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO gitlab_connections (
				id,version,label,origin,gitlab_project_id,path_with_namespace,path_lookup_key,
				canonical_web_url,default_branch,credential_ciphertext,encryption_key_id,
				status,last_validated_at,created_by,created_at,updated_at
			) VALUES ($1,1,'Migration repository','https://gitlab.example',42,
				'team/repository','team/repository','https://gitlab.example/team/repository',
				'main',$2,'key-1','active',$3,$4,$3,$3)`,
			connectionID, []byte("encrypted-token"), now, userA); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO gitlab_connection_events (
				id,connection_id,actor_user_id,event_type,outcome,origin,gitlab_project_id,
				request_id,created_at
			) VALUES ($1,$2,$3,'created','succeeded','https://gitlab.example',42,
				'migration-fixture',$4)`, eventID, connectionID, userA, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_repositories (id,project_id,connection_id,bound_by,bound_at)
			VALUES ($1,$2,$3,$4,$5)`, bindingID, projectResult.Project.ID, connectionID, userA, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_stage_claims (
				id,task_id,task_number,stage,claimed_by_type,claimed_by_user_id,
				subject_user_id,auth_method,status,outcome,version,expires_at,
				created_at,updated_at,completed_at
			) VALUES ($1,$2,$3,'execution','user',$4,$4,'session','completed',
				'execution_completed',1,$5,$6,$6,$6)`,
			claimID, taskResult.Task.ID, taskResult.Task.Number, userA, now.Add(time.Hour), now); err != nil {
			return err
		}
		for _, link := range []struct {
			id       uuid.UUID
			iid      int64
			provider int64
			unlinked bool
		}{{activeLinkID, 7, 7007, false}, {unlinkedLinkID, 8, 7008, true}} {
			var unlinkColumns string
			var unlinkValues []any
			if link.unlinked {
				unlinkColumns = ",unlinked_by_type,unlinked_by_user_id,unlinked_through_claim_id,unlinked_at"
				unlinkValues = []any{"user", userA, claimID, now.Add(time.Minute)}
			}
			query := `INSERT INTO task_merge_requests (
				id,task_id,project_id,project_repository_id,merge_request_iid,
				gitlab_merge_request_id,web_url,linked_by_type,linked_by_user_id,
				linked_through_claim_id,linked_at,observation_status,observed_at,title,
				state,draft,source_branch,target_branch,head_sha,provider_updated_at,
				created_at,updated_at` + unlinkColumns + `)
				VALUES ($1,$2,$3,$4,$5,$6,$7,'user',$8,$9,$10,'confirmed',$10,
				'Migration change','opened',false,'feature/migration','main','abc123',$10,$10,$10`
			args := []any{link.id, taskResult.Task.ID, projectResult.Project.ID, bindingID,
				link.iid, link.provider, "https://gitlab.example/team/repository/-/merge_requests/" +
					map[int64]string{7: "7", 8: "8"}[link.iid], userA, claimID, now}
			for index := range unlinkValues {
				query += ",$" + strconv.Itoa(len(args)+index+1)
			}
			query += ")"
			args = append(args, unlinkValues...)
			if _, err := tx.Exec(ctx, query, args...); err != nil {
				return err
			}
		}
		var mainThreadID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM task_threads WHERE task_id=$1 AND role='main'`, taskResult.Task.ID).Scan(&mainThreadID); err != nil {
			return err
		}
		codeChangeSnapshot := func(id uuid.UUID, number int64) map[string]any {
			return map[string]any{
				"task_merge_request_id": id, "project_repository_id": bindingID,
				"connection_id": connectionID, "gitlab_project_id": 42,
				"merge_request_iid": number,
				"web_url":           "https://gitlab.example/team/repository/-/merge_requests/" + strconv.FormatInt(number, 10),
				"title":             "Migration change", "state": "opened", "draft": false,
				"source_branch": "feature/migration", "target_branch": "main", "head_sha": "abc123",
				"observation_status": "confirmed", "observed_at": now,
			}
		}
		payload, err := json.Marshal(map[string]any{
			"review_cycle": 1, "submission_item_ids": []string{},
			"execution_check_ids": []string{}, "criterion_revisions": []string{},
			"merge_requests": []map[string]any{
				codeChangeSnapshot(activeLinkID, 7),
				codeChangeSnapshot(unlinkedLinkID, 8),
			},
		})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO task_thread_items (
				id,thread_id,kind,author_type,author_ref,body,typed_payload,request_id,
				task_stage_claim_id,task_review_cycle,created_at,updated_at
			) VALUES ($1,$2,'execution_completed','system','pactline-system',
				'Execution completed',$3,'migration-completion',$4,1,$5,$5)`,
			uuid.New(), mainThreadID, payload, claimID, now)
		return err
	}()
	if err != nil {
		_ = tx.Rollback(ctx)
	}
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, db.Migrate(ctx, pactline.MigrationFS))

	var provider, providerRepositoryID string
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT provider,provider_repository_id FROM repository_connections WHERE id=$1`,
		connectionID).Scan(&provider, &providerRepositoryID))
	require.Equal(t, "gitlab", provider)
	require.Equal(t, "42", providerRepositoryID)
	var projectRepositoryProvider, projectRepositoryOrigin, projectRepositoryPath string
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT provider,origin,path_with_namespace FROM project_repositories WHERE id=$1`,
		bindingID).Scan(&projectRepositoryProvider, &projectRepositoryOrigin, &projectRepositoryPath))
	require.Equal(t, "gitlab", projectRepositoryProvider)
	require.Equal(t, "https://gitlab.example", projectRepositoryOrigin)
	require.Equal(t, "team/repository", projectRepositoryPath)
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT provider,provider_repository_id FROM repository_connection_events WHERE id=$1`,
		eventID).Scan(&provider, &providerRepositoryID))
	require.Equal(t, "gitlab", provider)
	require.Equal(t, "42", providerRepositoryID)

	var totalLinks, activeLinks int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE unlinked_at IS NULL)
		FROM task_code_changes WHERE task_id=$1`, taskResult.Task.ID).Scan(&totalLinks, &activeLinks))
	require.Equal(t, 2, totalLinks)
	require.Equal(t, 1, activeLinks)
	var kind, providerChangeID string
	var changeNumber int64
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT kind,change_number,provider_change_id FROM task_code_changes WHERE id=$1`,
		activeLinkID).Scan(&kind, &changeNumber, &providerChangeID))
	require.Equal(t, "merge_request", kind)
	require.Equal(t, int64(7), changeNumber)
	require.Equal(t, "7007", providerChangeID)

	var migratedPayload []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT typed_payload FROM task_thread_items WHERE request_id='migration-completion'`).Scan(&migratedPayload))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(migratedPayload, &decoded))
	require.NotContains(t, decoded, "merge_requests")
	changes, ok := decoded["code_changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 2)
	snapshot := changes[0].(map[string]any)
	require.Equal(t, activeLinkID.String(), snapshot["task_code_change_id"])
	require.Equal(t, "gitlab", snapshot["provider"])
	require.Equal(t, "merge_request", snapshot["kind"])
	require.Equal(t, float64(7), snapshot["change_number"])
	evidence := snapshot["provider_evidence"].(map[string]any)
	require.Equal(t, "42", evidence["provider_repository_id"])
	require.Equal(t, "7007", evidence["provider_change_id"])
	require.Equal(t, connectionID.String(), evidence["connection_id"])
	require.NotContains(t, snapshot, "provider_repository_id")
	require.NotContains(t, snapshot, "provider_change_id")
	require.NotContains(t, snapshot, "task_merge_request_id")
	require.NotContains(t, snapshot, "gitlab_project_id")
	require.NotContains(t, snapshot, "merge_request_iid")

	active, err := store.NewTaskCodeChangeStore(db).ListActive(ctx, taskResult.Task.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, activeLinkID, active[0].CodeChange.ID)
	require.Equal(t, domain.RepositoryProviderGitLab, active[0].CodeChange.Provider)
	require.Equal(t, domain.CodeChangeKindMergeRequest, active[0].CodeChange.Kind)
	require.NotNil(t, active[0].CodeChange.ProviderEvidence)
	require.Equal(t, "7007", active[0].CodeChange.ProviderEvidence.ProviderChangeID)
	require.NotNil(t, active[0].CodeChange.ProviderVerification)
	require.Equal(t, domain.CodeChangeVerificationVerified, active[0].CodeChange.ProviderVerification.Status)
	frozen, err := store.NewTaskCodeChangeStore(db).GetReviewSnapshot(ctx, taskResult.Task.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, frozen)
	require.Len(t, frozen.CodeChanges, 2)
	require.Equal(t, activeLinkID, frozen.CodeChanges[0].TaskCodeChangeID)
	require.Equal(t, unlinkedLinkID, frozen.CodeChanges[1].TaskCodeChangeID)
	require.NotNil(t, frozen.CodeChanges[1].ProviderEvidence)
	require.Equal(t, "7008", frozen.CodeChanges[1].ProviderEvidence.ProviderChangeID)
	current := active[0].CodeChange
	require.Equal(t, "unchanged", application.DeliveryComparison(frozen.CodeChanges[0], &current))
	require.Equal(t, "missing", application.DeliveryComparison(frozen.CodeChanges[1], nil))
	var legacyConstraintNames int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conrelid IN (
			'repository_connections'::regclass,
			'repository_connection_events'::regclass,
			'task_code_changes'::regclass
		)
		AND (
			conname LIKE 'gitlab_connections_%'
			OR conname LIKE 'gitlab_connection_events_%'
			OR conname LIKE 'task_merge_requests_%'
		)`).Scan(&legacyConstraintNames))
	require.Zero(t, legacyConstraintNames)
}
