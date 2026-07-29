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
	"sync/atomic"
	"testing"
	"time"

	"bountyboard"
	contract "bountyboard/api"
	"bountyboard/internal/access"
	"bountyboard/internal/api"
	apiv1 "bountyboard/internal/api/v1"
	"bountyboard/internal/application"
	"bountyboard/internal/identity"
	"bountyboard/internal/integrations/devauth"
	legacyapi "bountyboard/internal/legacy/api"
	legacystore "bountyboard/internal/legacy/store"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	userA = "00000000-0000-0000-0000-000000000001" // 产品 A
	userB = "00000000-0000-0000-0000-000000000002" // 技术 Leader B
	userC = "00000000-0000-0000-0000-000000000003" // 研发 C
	userD = "00000000-0000-0000-0000-000000000004" // 研发 D
)

// skippedForNoDatabase counts tests in this package that skipped for lack of
// DATABASE_URL, so TestMain can print a warning no one can mistake for a
// clean, full run. Mirrors internal/legacy/api's own testDSN/TestMain.
var skippedForNoDatabase atomic.Int64

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
	require.NoError(t, db.Migrate(context.Background(), bountyboard.MigrationFS))
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

	users := store.NewUserStore(db)
	tasks := store.NewTaskStore(db)
	comments := store.NewCommentStore(db)
	labels := store.NewLabelStore(db)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	projectService := &application.ProjectService{
		Projects: projects, Milestones: milestones, Acceptance: acceptance, Tasks: tasks,
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
	accessAuditStore := store.NewAccessAuditStore(db)
	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)
	taskSurface := &api.TaskSurface{
		Tasks: tasks, Comments: comments, Labels: labels,
		Projects: projects, Milestones: milestones, Acceptance: acceptance,
		ProjectService: projectService,
	}
	v1Handler, err := apiv1.NewServer(&apiv1.Handler{Users: users})
	require.NoError(t, err)
	h := api.NewRouter(users, legacyHandler, api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: identityService, Tokens: tokenService,
			AccessAudit: accessAuditStore, Idempotency: store.NewIdempotencyStore(db),
			Development: devauth.New(users, identityService), AppBaseURL: baseURL,
		},
		Tasks:   taskSurface,
		V1:      v1Handler,
		OpenAPI: apiv1.OpenAPIHandler(contract.OpenAPIDocument),
	})
	return h, db
}

// The HTTP suite exercises Development sessions with all six historical role
// fixtures. The identity migration intentionally deactivates five of them, so
// expose them only for each test and restore the migrated state afterward.
func enableLegacySeedIdentities(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, `
		UPDATE users
		SET active = true, updated_at = now()
		WHERE id IN (
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000003',
			'00000000-0000-0000-0000-000000000004',
			'00000000-0000-0000-0000-000000000005',
			'00000000-0000-0000-0000-000000000006'
		)
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `
			UPDATE users
			SET active = false, updated_at = now()
			WHERE id IN (
				'00000000-0000-0000-0000-000000000002',
				'00000000-0000-0000-0000-000000000003',
				'00000000-0000-0000-0000-000000000004',
				'00000000-0000-0000-0000-000000000005',
				'00000000-0000-0000-0000-000000000006'
			)
		`)
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

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(dst))
}

// taskResponse mirrors internal/api's unexported taskView, so this package's
// (black-box, api_test) tests can decode what the HTTP layer actually sent
// without importing an unexported type.
type taskResponse struct {
	ID          uuid.UUID              `json:"id"`
	Number      int64                  `json:"number"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Priority    string                 `json:"priority"`
	Assignee    *userRefJSON           `json:"assignee"`
	Creator     userRefJSON            `json:"creator"`
	DueDate     *string                `json:"due_date"`
	Project     *taskProjectResponse   `json:"project"`
	Milestone   *taskMilestoneResponse `json:"milestone"`
	Labels      []labelJSON            `json:"labels"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at"`
	ArchivedAt  *time.Time             `json:"archived_at"`
}

type taskProjectResponse struct {
	ID     uuid.UUID `json:"id"`
	Number int64     `json:"number"`
	Name   string    `json:"name"`
}

type taskMilestoneResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type userRefJSON struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

type labelJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type taskListResponseJSON struct {
	Items      []taskResponse `json:"items"`
	NextCursor string         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

type commentResponse struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	AuthorID  uuid.UUID `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type activityResponse struct {
	ID        uuid.UUID `json:"id"`
	ActorID   uuid.UUID `json:"actor_id"`
	Field     string    `json:"field"`
	OldValue  *string   `json:"old_value"`
	NewValue  *string   `json:"new_value"`
	CreatedAt time.Time `json:"created_at"`
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
		_, err = db.Pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

func cleanupLabelRow(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM labels WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

// mustCreateTaskHTTP is a small helper most handler tests use to get a task
// on the board without repeating the create-and-decode boilerplate.
func mustCreateTaskHTTP(t *testing.T, h http.Handler, db *store.DB, userID string, body map[string]any) taskResponse {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/tasks", userID, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out taskResponse
	decodeJSON(t, rec, &out)
	cleanupTaskRow(t, db, out.ID)
	return out
}
