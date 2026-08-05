package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/api"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/integrations/devauth"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type failingLogoutSessions struct {
	session identity.Session
	user    domain.User
}

func (f *failingLogoutSessions) CreateSession(_ context.Context, session identity.Session, _ identity.AuditEvent) error {
	f.session = session
	return nil
}

func (f *failingLogoutSessions) ResolveSession(
	context.Context,
	uuid.UUID,
	[]byte,
	time.Time,
) (identity.SessionBundle, error) {
	return identity.SessionBundle{Session: f.session, User: f.user}, nil
}

func (f *failingLogoutSessions) TouchSession(context.Context, uuid.UUID, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (f *failingLogoutSessions) LogoutSession(context.Context, uuid.UUID, time.Time, string) error {
	return errors.New("forced logout failure")
}

func developmentLogin(t *testing.T, handler http.Handler, userID string) ([]*http.Cookie, string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"user_id": userID})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/dev/session", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	var csrf string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "bb_csrf" {
			csrf = cookie.Value
		}
	}
	require.NotEmpty(t, csrf)
	return response.Result().Cookies(), csrf
}

func authenticatedHTTPTestRequest(method, path string, body []byte, cookies []*http.Cookie, csrf string) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		request.Header.Set("Origin", "http://app.test")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("X-CSRF-Token", csrf)
	}
	return request
}

func TestHeaderCannotAuthenticate(t *testing.T) {
	handler, _ := newTaskTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.Header.Set("X-User-Id", userA)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestDevelopmentSessionCookieFlagsAndHashPersistence(t *testing.T) {
	handler, db := newTaskTestServer(t)
	cookies, _ := developmentLogin(t, handler, userA)
	require.Len(t, cookies, 2)
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	sessionCookie := byName["bb_session"]
	csrfCookie := byName["bb_csrf"]
	require.NotNil(t, sessionCookie)
	require.True(t, sessionCookie.HttpOnly)
	require.False(t, sessionCookie.Secure)
	require.Equal(t, "/", sessionCookie.Path)
	require.Equal(t, http.SameSiteLaxMode, sessionCookie.SameSite)
	require.NotNil(t, csrfCookie)
	require.False(t, csrfCookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, csrfCookie.SameSite)

	parts := strings.Split(sessionCookie.Value, ".")
	require.Len(t, parts, 3)
	sessionID := uuid.MustParse(parts[0])
	var persisted []byte
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT secret_hash FROM sessions WHERE id=$1`, sessionID).Scan(&persisted))
	require.NotEqual(t, []byte(parts[1]), persisted)
	require.Len(t, persisted, 32)
}

func TestMutationRequiresSessionCSRFAndSameOrigin(t *testing.T) {
	handler, _ := newTaskTestServer(t)
	cookies, csrf := developmentLogin(t, handler, userA)
	request := authenticatedHTTPTestRequest(http.MethodPost, "/api/v1/tasks", []byte(`{"title":"blocked"}`), cookies, csrf)
	request.Header.Del("X-CSRF-Token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	request = authenticatedHTTPTestRequest(http.MethodPost, "/api/v1/tasks", []byte(`{"title":"blocked"}`), cookies, csrf)
	request.Header.Set("Origin", "https://evil.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestMeReturnsActorSubjectAndNullableProfileFields(t *testing.T) {
	handler, db := newTaskTestServer(t)
	var originalEmail, originalAvatarURL *string
	var originalPlatformRole domain.PlatformRole
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT email, avatar_url, platform_role FROM users WHERE id=$1`, userA).
		Scan(&originalEmail, &originalAvatarURL, &originalPlatformRole))
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET email=NULL, avatar_url=NULL WHERE id=$1`, userA)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET email=$2, avatar_url=$3 WHERE id=$1`,
			userA, originalEmail, originalAvatarURL)
		require.NoError(t, cleanupErr)
	})
	cookies, csrf := developmentLogin(t, handler, userA)
	request := authenticatedHTTPTestRequest(http.MethodGet, "/api/me", nil, cookies, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Actor struct {
			ID           uuid.UUID           `json:"id"`
			Email        *string             `json:"email"`
			AvatarURL    *string             `json:"avatar_url"`
			PlatformRole domain.PlatformRole `json:"platform_role"`
			CreatedAt    time.Time           `json:"created_at"`
			UpdatedAt    time.Time           `json:"updated_at"`
		} `json:"actor"`
		Subject struct {
			ID uuid.UUID `json:"id"`
		} `json:"subject"`
		Impersonation any `json:"impersonation"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, uuid.MustParse(userA), body.Actor.ID)
	require.Equal(t, body.Actor.ID, body.Subject.ID)
	require.NotZero(t, body.Actor.CreatedAt)
	require.NotZero(t, body.Actor.UpdatedAt)
	require.Nil(t, body.Actor.Email)
	require.Nil(t, body.Actor.AvatarURL)
	require.Equal(t, originalPlatformRole, body.Actor.PlatformRole)
	require.Nil(t, body.Impersonation)
}

func TestPendingMemberSessionIsRestrictedUntilAdministratorApproval(t *testing.T) {
	handler, db := newTaskTestServer(t)
	ctx := context.Background()
	adminID, memberID := uuid.MustParse(userA), uuid.MustParse(userB)
	type originalUserState struct {
		Role         domain.PlatformRole
		AccessStatus domain.AccessStatus
		Active       bool
	}
	loadState := func(id uuid.UUID) originalUserState {
		var state originalUserState
		require.NoError(t, db.Pool.QueryRow(ctx, `
			SELECT platform_role, access_status, active FROM users WHERE id=$1`, id).
			Scan(&state.Role, &state.AccessStatus, &state.Active))
		return state
	}
	adminOriginal, memberOriginal := loadState(adminID), loadState(memberID)
	_, err := db.Pool.Exec(ctx, `
		UPDATE users SET platform_role='ADMIN', access_status='APPROVED', active=true WHERE id=$1`, adminID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		UPDATE users SET platform_role='MEMBER', access_status='PENDING', active=true WHERE id=$1`, memberID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `
			UPDATE users SET platform_role=$2, access_status=$3, active=$4 WHERE id=$1`,
			adminID, adminOriginal.Role, adminOriginal.AccessStatus, adminOriginal.Active)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(context.Background(), `
			UPDATE users SET platform_role=$2, access_status=$3, active=$4 WHERE id=$1`,
			memberID, memberOriginal.Role, memberOriginal.AccessStatus, memberOriginal.Active)
		require.NoError(t, cleanupErr)
	})

	memberCookies, memberCSRF := developmentLogin(t, handler, memberID.String())
	meRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/me", nil, memberCookies, memberCSRF)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, meRequest)
	require.Equal(t, http.StatusOK, meResponse.Code, meResponse.Body.String())
	require.Contains(t, meResponse.Body.String(), `"access_status":"PENDING"`)

	blockedRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/v1/users", nil, memberCookies, memberCSRF)
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blockedRequest)
	require.Equal(t, http.StatusForbidden, blockedResponse.Code, blockedResponse.Body.String())
	require.Contains(t, blockedResponse.Body.String(), "access approval required")

	adminCookies, adminCSRF := developmentLogin(t, handler, adminID.String())
	approveRequest := authenticatedHTTPTestRequest(
		http.MethodPatch,
		"/api/admin/users/"+memberID.String(),
		[]byte(`{"access_status":"APPROVED"}`),
		adminCookies,
		adminCSRF,
	)
	approveResponse := httptest.NewRecorder()
	handler.ServeHTTP(approveResponse, approveRequest)
	require.Equal(t, http.StatusNoContent, approveResponse.Code, approveResponse.Body.String())

	allowedRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/v1/users", nil, memberCookies, memberCSRF)
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowedRequest)
	require.Equal(t, http.StatusOK, allowedResponse.Code, allowedResponse.Body.String())
}

func TestMePropagatesImpersonationActorAndSubject(t *testing.T) {
	handler, db := newTaskTestServer(t)
	ctx := context.Background()
	var originalPlatformRole domain.PlatformRole
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT platform_role FROM users WHERE id=$1`, userA).Scan(&originalPlatformRole))
	_, err := db.Pool.Exec(ctx, `UPDATE users SET platform_role='ADMIN' WHERE id=$1`, userA)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET platform_role=$2 WHERE id=$1`, userA, originalPlatformRole)
		require.NoError(t, cleanupErr)
	})
	cookies, csrf := developmentLogin(t, handler, userA)
	sessionParts := strings.Split(cookies[0].Value, ".")
	sessionID := uuid.MustParse(sessionParts[0])
	impersonationID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO impersonations (id,session_id,actor_user_id,subject_user_id,started_at)
		VALUES ($1,$2,$3,$4,now())`,
		impersonationID, sessionID, userA, userB)
	require.NoError(t, err)

	request := authenticatedHTTPTestRequest(http.MethodGet, "/api/me", nil, cookies, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Actor struct {
			ID uuid.UUID `json:"id"`
		} `json:"actor"`
		Subject struct {
			ID uuid.UUID `json:"id"`
		} `json:"subject"`
		Impersonation struct {
			ID uuid.UUID `json:"id"`
		} `json:"impersonation"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, uuid.MustParse(userA), body.Actor.ID)
	require.Equal(t, uuid.MustParse(userB), body.Subject.ID)
	require.Equal(t, impersonationID, body.Impersonation.ID)
}

func TestLogoutRevokesSessionAndClearsCookies(t *testing.T) {
	handler, db := newTaskTestServer(t)
	cookies, csrf := developmentLogin(t, handler, userA)
	sessionID := uuid.MustParse(strings.Split(cookies[0].Value, ".")[0])
	request := authenticatedHTTPTestRequest(http.MethodPost, "/api/auth/logout", nil, cookies, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	var revokedAt *time.Time
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT revoked_at FROM sessions WHERE id=$1`, sessionID).Scan(&revokedAt))
	require.NotNil(t, revokedAt)
	for _, cookie := range response.Result().Cookies() {
		require.Less(t, cookie.MaxAge, 0)
	}
}

func TestLogoutClearsCookiesWhenPersistenceFails(t *testing.T) {
	_, db := newTaskTestServer(t)
	users := store.NewUserStore(db)
	user, err := users.GetByID(context.Background(), uuid.MustParse(userA))
	require.NoError(t, err)
	repository := &failingLogoutSessions{user: user}
	service, err := identity.NewService(
		repository, users, []byte("01234567890123456789012345678901"),
		identity.SystemClock{}, identity.CryptoSecretGenerator{},
	)
	require.NoError(t, err)
	baseURL, err := url.Parse("http://app.test")
	require.NoError(t, err)
	handler := api.NewRouter(http.NotFoundHandler(), api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: service, Development: devauth.New(users, service), AppBaseURL: baseURL,
		},
	})
	cookies, csrf := developmentLogin(t, handler, userA)
	request := authenticatedHTTPTestRequest(http.MethodPost, "/api/auth/logout", nil, cookies, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	cleared := map[string]bool{}
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			cleared[cookie.Name] = true
		}
	}
	require.True(t, cleared["bb_session"])
	require.True(t, cleared["bb_csrf"])
}

func TestDevelopmentSessionRejectsInactiveUser(t *testing.T) {
	handler, db := newTaskTestServer(t)
	require.NoError(t, store.NewUserStore(db).SetActive(context.Background(), uuid.MustParse(userB), false))
	body, err := json.Marshal(map[string]string{"user_id": userB})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/dev/session", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
