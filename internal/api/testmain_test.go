package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wolfhead/pactline"
	contract "github.com/wolfhead/pactline/api"
	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/api"
	apiv1 "github.com/wolfhead/pactline/internal/api/v1"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/blob"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/integrations/devauth"
	githubintegration "github.com/wolfhead/pactline/internal/integrations/github"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"
	"github.com/wolfhead/pactline/internal/integrations/repositoryfixture"
	legacyapi "github.com/wolfhead/pactline/internal/legacy/api"
	legacystore "github.com/wolfhead/pactline/internal/legacy/store"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	userA = "00000000-0000-0000-0000-000000000001" // Product owner fixture
	userB = "00000000-0000-0000-0000-000000000002" // Technical lead fixture
	userC = "00000000-0000-0000-0000-000000000003" // Engineer fixture
	userD = "00000000-0000-0000-0000-000000000004" // Engineer fixture
)

// skippedForNoDatabase counts tests in this package that skipped for lack of
// DATABASE_URL, so TestMain can print a warning no one can mistake for a
// clean, full run. Mirrors internal/legacy/api's own testDSN/TestMain.
var skippedForNoDatabase atomic.Int64

type apiTestGitLabProvider struct {
	mu                sync.Mutex
	pathWithNamespace string
	mergeRequestCalls map[int64]int
}

type apiTestGitHubProvider struct {
	mu                sync.Mutex
	pathWithNamespace string
}

func (*apiTestGitHubProvider) Provider() domain.RepositoryProvider {
	return domain.RepositoryProviderGitHub
}

func (*apiTestGitHubProvider) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	return githubintegration.NewClient(nil, time.Second).ParseRepositoryURL(raw)
}

func (*apiTestGitHubProvider) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	return githubintegration.NewClient(nil, time.Second).ParseCodeChangeURL(raw)
}

func (p *apiTestGitHubProvider) ResolveRepository(
	_ context.Context,
	reference domain.RepositoryReference,
	_ []byte,
	_ string,
) (domain.RepositoryIdentity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pathWithNamespace = reference.PathWithNamespace
	return domain.RepositoryIdentity{
		ProviderRepositoryID: "52001", PathWithNamespace: reference.PathWithNamespace,
		WebURL: reference.Origin + "/" + reference.PathWithNamespace, DefaultBranch: "main",
	}, nil
}

func (p *apiTestGitHubProvider) GetCodeChange(
	_ context.Context,
	reference domain.RepositoryReference,
	_ string,
	kind domain.CodeChangeKind,
	changeNumber int64,
	_ []byte,
	_ string,
) (domain.CodeChange, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if kind != domain.CodeChangeKindPullRequest {
		return domain.CodeChange{}, domain.ErrInvalidInput
	}
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	return domain.CodeChange{
		Provider: domain.RepositoryProviderGitHub, Kind: domain.CodeChangeKindPullRequest,
		ProviderChangeID: fmt.Sprintf("%d", 92000+changeNumber), ChangeNumber: changeNumber,
		WebURL: fmt.Sprintf("%s/%s/pull/%d", reference.Origin, reference.PathWithNamespace, changeNumber),
		Observation: domain.CodeChangeObservation{
			Status: domain.CodeChangeObservationConfirmed, ObservedAt: now,
			Title: "GitHub API delivery evidence", State: domain.CodeChangeStateOpened,
			SourceBranch: "feature/github-evidence", TargetBranch: "main", HeadSHA: "fedcba654321",
			ProviderUpdatedAt: now,
		},
	}, nil
}

func (*apiTestGitLabProvider) Provider() domain.RepositoryProvider {
	return domain.RepositoryProviderGitLab
}

func (*apiTestGitLabProvider) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	return gitlabintegration.NewClient(nil, time.Second).ParseRepositoryURL(raw)
}

func (*apiTestGitLabProvider) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	return gitlabintegration.NewClient(nil, time.Second).ParseCodeChangeURL(raw)
}

func (p *apiTestGitLabProvider) ResolveRepository(
	_ context.Context,
	reference domain.RepositoryReference,
	_ []byte,
	_ string,
) (domain.RepositoryIdentity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pathWithNamespace = reference.PathWithNamespace
	return domain.RepositoryIdentity{
		ProviderRepositoryID: "42001", PathWithNamespace: reference.PathWithNamespace,
		WebURL: reference.Origin + "/" + reference.PathWithNamespace, DefaultBranch: "main",
	}, nil
}

func (p *apiTestGitLabProvider) GetCodeChange(
	_ context.Context,
	reference domain.RepositoryReference,
	_ string,
	kind domain.CodeChangeKind,
	changeNumber int64,
	_ []byte,
	_ string,
) (domain.CodeChange, error) {
	origin := reference.Origin
	p.mu.Lock()
	defer p.mu.Unlock()
	if kind != domain.CodeChangeKindMergeRequest {
		return domain.CodeChange{}, domain.ErrInvalidInput
	}
	if p.mergeRequestCalls == nil {
		p.mergeRequestCalls = make(map[int64]int)
	}
	p.mergeRequestCalls[changeNumber]++
	if changeNumber == 503 && p.mergeRequestCalls[changeNumber] > 1 {
		return domain.CodeChange{}, &gitlabintegration.ProviderError{
			Category: gitlabintegration.ErrorUnreachable,
		}
	}
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	return domain.CodeChange{
		Provider: domain.RepositoryProviderGitLab, Kind: domain.CodeChangeKindMergeRequest,
		ProviderChangeID: fmt.Sprintf("%d", 91000+changeNumber), ChangeNumber: changeNumber,
		WebURL: fmt.Sprintf("%s/%s/-/merge_requests/%d", origin, p.pathWithNamespace, changeNumber),
		Observation: domain.CodeChangeObservation{
			Status: domain.CodeChangeObservationConfirmed, ObservedAt: now,
			Title: "API delivery evidence", State: domain.CodeChangeStateOpened,
			SourceBranch: "feature/evidence", TargetBranch: "main", HeadSHA: "abc123def456",
			ProviderUpdatedAt: now,
		},
	}, nil
}

func newTaskTestServer(t *testing.T) (http.Handler, *store.DB) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL not set while CI is set: refusing to silently skip API integration tests. Run via `make test`.")
		}
		skippedForNoDatabase.Add(1)
		t.Skip("DATABASE_URL not set; run via `make test`")
	}
	db, err := store.Connect(context.Background(), dsn)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), pactline.MigrationFS))
	t.Cleanup(db.Close)
	sessionCutoff := time.Now().UTC()
	t.Cleanup(func() {
		ctx := context.Background()
		_, cleanupErr := db.Pool.Exec(ctx, `
			DELETE FROM identity_audit_events
			WHERE session_id IN (SELECT id FROM sessions WHERE created_at >= $1)`, sessionCutoff)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(ctx, `
			DELETE FROM impersonations
			WHERE session_id IN (SELECT id FROM sessions WHERE created_at >= $1)`, sessionCutoff)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(ctx, `DELETE FROM sessions WHERE created_at >= $1`, sessionCutoff)
		require.NoError(t, cleanupErr)
	})
	enableLegacySeedIdentities(t, db)
	ensureAPITestProject(t, db)

	users := store.NewUserStore(db)
	tasks := store.NewTaskStore(db)
	comments := store.NewCommentStore(db)
	labels := store.NewLabelStore(db)
	projects := store.NewProjectStore(db)
	memberships := store.NewProjectMembershipStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	attachmentObjects, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	attachmentService := &application.AttachmentService{
		Attachments: store.NewAttachmentStore(db), Objects: attachmentObjects,
	}
	projectService := &application.ProjectService{
		Projects: projects, Milestones: milestones, Acceptance: acceptance, Tasks: tasks,
	}
	projectAccess := &application.ProjectAccessService{
		Projects: projects, Tasks: tasks, Memberships: memberships,
	}
	gitLabCipher, err := identity.NewCredentialCipher(map[string][]byte{
		"api-test-gitlab-key": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	gitLabProvider, err := repositoryfixture.New(
		domain.RepositoryProviderGitLab, &apiTestGitLabProvider{},
	)
	require.NoError(t, err)
	gitHubProvider, err := repositoryfixture.New(
		domain.RepositoryProviderGitHub, &apiTestGitHubProvider{},
	)
	require.NoError(t, err)
	repositoryProviders, err := application.NewRepositoryProviderRegistry(gitLabProvider, gitHubProvider)
	require.NoError(t, err)
	repositoryConnectionService := &application.RepositoryConnectionService{
		Connections: store.NewRepositoryConnectionStore(db),
		Providers:   repositoryProviders,
		Cipher:      gitLabCipher, EncryptionKeyID: "api-test-gitlab-key",
		Now: time.Now,
	}
	projectRepositoryService := &application.ProjectRepositoryService{
		Repositories:      store.NewProjectRepositoryStore(db),
		Connections:       store.NewRepositoryConnectionStore(db),
		ConnectionService: repositoryConnectionService,
		Providers:         repositoryProviders,
		Access:            projectAccess,
		Now:               time.Now,
	}
	taskDeliveryService := &application.TaskDeliveryService{
		CodeChanges:  store.NewTaskCodeChangeStore(db),
		Repositories: store.NewProjectRepositoryStore(db),
		Access:       projectAccess,
		Providers:    repositoryProviders,
		Cipher:       gitLabCipher,
		Now:          time.Now,
	}
	taskThreadStore := store.NewTaskThreadStore(db)
	taskWorkPacketService := &application.TaskWorkPacketService{
		Access: projectAccess, Acceptance: acceptance, Threads: taskThreadStore,
		Delivery: taskDeliveryService,
	}
	agentRuns := store.NewAgentStore(db)
	agentConversations := store.NewAgentConversationStore(db)
	agentConversationService := &application.AgentConversationService{
		Conversations: agentConversations,
		Projects:      projects,
		Access:        projectAccess,
		Now:           time.Now,
	}
	legacyHandler := legacyapi.NewRouter(
		users, legacystore.NewBountyStore(db), legacystore.NewCreditStore(db),
		legacystore.NewCalibrationStore(db), legacystore.NewAnchorStore(db),
	)
	identityService, err := identity.NewService(
		store.NewIdentityStore(db), users, []byte("api-tests-session-secret-32-byte"),
		identity.SystemClock{}, identity.CryptoSecretGenerator{},
	)
	require.NoError(t, err)
	tokenService := access.NewService(
		store.NewAccessStore(db), identity.SystemClock{}, access.CryptoSecretGenerator{},
	)
	delegateService := newTestDelegateService(t, db)
	accessAuditStore := store.NewAccessAuditStore(db)
	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)
	v1Handler, err := apiv1.NewServer(&apiv1.Handler{
		Users: users,
		Tasks: &application.TaskService{
			Tasks: tasks, Comments: comments, Projects: projectService,
		},
		Workflow:            store.NewTaskWorkflowStore(db),
		StageClaims:         store.NewTaskStageClaimStore(db),
		Threads:             taskThreadStore,
		Labels:              &application.LabelService{Labels: labels},
		Projects:            projectService,
		ProjectRepositories: projectRepositoryService,
		Delivery:            taskDeliveryService,
		WorkPackets:         taskWorkPacketService,
		Access:              projectAccess,
		Attachments:         attachmentService,
		AgentConversations:  agentConversationService,
		AgentRuns:           agentRuns,
	})
	require.NoError(t, err)
	h := api.NewRouter(legacyHandler, api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: identityService, Tokens: tokenService,
			Delegates:   delegateService,
			AccessAudit: accessAuditStore, LarkAudit: accessAuditStore,
			Idempotency: store.NewIdempotencyStore(db),
			Development: devauth.New(users, identityService), AppBaseURL: baseURL,
		},
		V1:                         v1Handler,
		OpenAPI:                    apiv1.OpenAPIHandler(contract.OpenAPIDocument),
		AdminRepositoryConnections: repositoryConnectionService,
	})
	return h, db
}

func newTestDelegateService(t *testing.T, db *store.DB) *access.DelegateService {
	t.Helper()
	service, err := access.NewDelegateService(access.DelegateConfig{
		ActiveKeyID: "api-test-agent-key",
		SigningKeys: map[string][]byte{
			"api-test-agent-key": []byte("api-test-agent-delegation-key-32b"),
		},
	}, store.NewAgentStore(db), store.NewUserStore(db), identity.SystemClock{})
	require.NoError(t, err)
	return service
}

// The HTTP suite exercises Development sessions with all six historical role
// fixtures. It also treats the primary identity as the single administrator.
// Restore the migrated state after each test so the shared test database does
// not accumulate authorization state.
func enableLegacySeedIdentities(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := context.Background()
	var originalPrimaryRole string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT platform_role FROM users WHERE id=$1`, userA,
	).Scan(&originalPrimaryRole))
	_, err := db.Pool.Exec(ctx, `
		UPDATE users
		SET active = true,
		    platform_role = CASE WHEN id=$1 THEN 'ADMIN' ELSE platform_role END,
		    updated_at = now()
		WHERE id IN (
			'00000000-0000-0000-0000-000000000001',
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000003',
			'00000000-0000-0000-0000-000000000004',
			'00000000-0000-0000-0000-000000000005',
			'00000000-0000-0000-0000-000000000006'
		)
	`, userA)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `
			UPDATE users
			SET active = CASE WHEN id=$1 THEN true ELSE false END,
			    platform_role = CASE WHEN id=$1 THEN $2 ELSE platform_role END,
			    updated_at = now()
			WHERE id IN (
				'00000000-0000-0000-0000-000000000001',
				'00000000-0000-0000-0000-000000000002',
				'00000000-0000-0000-0000-000000000003',
				'00000000-0000-0000-0000-000000000004',
				'00000000-0000-0000-0000-000000000005',
				'00000000-0000-0000-0000-000000000006'
			)
		`, userA, originalPrimaryRole)
		require.NoError(t, cleanupErr)
	})
}

// Fresh databases contain no Project until real work is created. API tests
// that create Tasks without first exercising Project creation still need one
// explicit workspace fixture; a developer's pre-existing local data must not
// be what makes the suite pass.
func ensureAPITestProject(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := context.Background()
	var exists bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE archived_at IS NULL)`,
	).Scan(&exists))
	if !exists {
		projectID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO projects (id, name, description, creator_id)
			VALUES ($1, $2, $3, $4)
		`, projectID, "API test workspace", "Workspace fixture for API integration tests", userA)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, cleanupErr := db.Pool.Exec(context.Background(),
				`DELETE FROM projects WHERE id=$1`, projectID)
			require.NoError(t, cleanupErr)
		})
	}

	rows, err := db.Pool.Query(ctx, `
		INSERT INTO project_memberships (id, project_id, user_id, role)
		SELECT gen_random_uuid(), p.id, u.id,
			CASE WHEN u.id=$1 THEN 'admin' ELSE 'member' END
		FROM projects p CROSS JOIN users u
		WHERE p.archived_at IS NULL AND u.active
		ON CONFLICT (project_id, user_id) DO NOTHING
		RETURNING id`, userA)
	require.NoError(t, err)
	var insertedMembershipIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		insertedMembershipIDs = append(insertedMembershipIDs, id)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	t.Cleanup(func() {
		if len(insertedMembershipIDs) == 0 {
			return
		}
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`DELETE FROM project_memberships WHERE id=ANY($1::uuid[])`, insertedMembershipIDs)
		require.NoError(t, cleanupErr)
	})
}

func TestMain(m *testing.M) {
	code := m.Run()
	if n := skippedForNoDatabase.Load(); n > 0 {
		bar := strings.Repeat("!", 78)
		fmt.Fprintf(os.Stderr, "\n%s\n%d test(s) in internal/api SKIPPED: DATABASE_URL was not set.\n"+
			"This is NOT a full run of this package's regression guards. Run `make\n"+
			"test` from the repo root instead.\n%s\n\n", bar, n, bar)
	}
	os.Exit(code)
}

func do(t *testing.T, h http.Handler, method, path, userID string, body any) *httptest.ResponseRecorder {
	return doWithHeaders(t, h, method, path, userID, nil, body)
}

func doWithHeaders(
	t *testing.T,
	h http.Handler,
	method, path, userID string,
	headers http.Header,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var cookies []*http.Cookie
	csrfToken := ""
	if userID != "" {
		loginBody, err := json.Marshal(map[string]string{"user_id": userID})
		require.NoError(t, err)
		loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/dev/session", bytes.NewReader(loginBody))
		loginRequest.Header.Set("Content-Type", "application/json")
		loginResponse := httptest.NewRecorder()
		h.ServeHTTP(loginResponse, loginRequest)
		require.Equal(t, http.StatusNoContent, loginResponse.Code, loginResponse.Body.String())
		cookies = loginResponse.Result().Cookies()
		for _, cookie := range cookies {
			if cookie.Name == "bb_csrf" {
				csrfToken = cookie.Value
			}
		}
	}
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && userID != "" {
		req.Header.Set("Origin", "http://app.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doRaw(
	t *testing.T,
	h http.Handler,
	method, path, userID, mediaType string,
	headers http.Header,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	loginBody, err := json.Marshal(map[string]string{"user_id": userID})
	require.NoError(t, err)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/dev/session", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	h.ServeHTTP(loginResponse, loginRequest)
	require.Equal(t, http.StatusNoContent, loginResponse.Code, loginResponse.Body.String())
	cookies := loginResponse.Result().Cookies()
	csrfToken := ""
	for _, cookie := range cookies {
		if cookie.Name == "bb_csrf" {
			csrfToken = cookie.Value
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.Header.Set("Content-Type", mediaType)
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Origin", "http://app.test")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(dst))
}

func cleanupTaskRow(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `
			DELETE FROM acceptance_checks
			WHERE criterion_id IN (
				SELECT id FROM acceptance_criteria WHERE task_id=$1
			)`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM acceptance_criteria WHERE task_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM task_code_changes WHERE task_id=$1`, id)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

func cleanupProjectRows(t *testing.T, db *store.DB, projectID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		statements := []string{
			`DELETE FROM task_code_changes WHERE project_id=$1`,
			`DELETE FROM project_repositories WHERE project_id=$1`,
			`DELETE FROM acceptance_checks WHERE criterion_id IN (
				SELECT id FROM acceptance_criteria
				WHERE milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
				   OR task_id IN (SELECT id FROM tasks WHERE project_id=$1)
			)`,
			`DELETE FROM acceptance_criteria
			 WHERE milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
			    OR task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM task_comments WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM task_activity WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM project_activity WHERE project_id=$1`,
			`DELETE FROM tasks WHERE project_id=$1`,
			`DELETE FROM milestones WHERE project_id=$1`,
			`DELETE FROM projects WHERE id=$1`,
		}
		for _, statement := range statements {
			_, err := db.Pool.Exec(ctx, statement, projectID)
			require.NoError(t, err)
		}
	})
}

func activeProjectNumber(t *testing.T, db *store.DB) int64 {
	t.Helper()
	var number int64
	err := db.Pool.QueryRow(context.Background(), `
		SELECT number
		FROM projects
		WHERE archived_at IS NULL
		ORDER BY CASE WHEN name='待整理' THEN 0 ELSE 1 END, number
		LIMIT 1`).Scan(&number)
	require.NoError(t, err)
	return number
}
