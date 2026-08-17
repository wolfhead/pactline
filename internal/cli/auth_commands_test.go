package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevelopmentAuthCreatesSessionAndStoresTokenWithoutPrintingIt(t *testing.T) {
	const token = "bb_pat_development-secret"
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		switch request.URL.Path {
		case "/api/auth/dev/session":
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, primaryDevelopmentUserID, body["user_id"])
			http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "session", Path: "/", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "bb_csrf", Value: "csrf-secret", Path: "/"})
			w.WriteHeader(http.StatusNoContent)
		case "/api/account/tokens":
			cookie, err := request.Cookie("bb_session")
			require.NoError(t, err)
			require.Equal(t, "session", cookie.Value)
			require.Equal(t, "csrf-secret", request.Header.Get("X-CSRF-Token"))
			require.Equal(t, "same-origin", request.Header.Get("Sec-Fetch-Site"))
			require.Equal(t, "http://"+request.Host, request.Header.Get("Origin"))
			var body developmentTokenRequest
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, developmentTokenRequest{
				Name: developmentTokenName, Scopes: []string{"work:execute"}, ExpiresInDays: 30,
			}, body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"55209633-ce9c-40bc-b5ca-9284f8c623c9","token":"` + token + `"}`))
		case "/api/auth/logout":
			require.Equal(t, "csrf-secret", request.Header.Get("X-CSRF-Token"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request path %s", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "pactline", "config.json")
	t.Setenv("PACTLINE_CONFIG", configPath)
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"auth", "development", "--server", server.URL,
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	require.Equal(t, []string{"/api/auth/dev/session", "/api/account/tokens", "/api/auth/logout"}, paths)
	require.NotContains(t, stdout.String()+stderr.String(), token)
	require.Contains(t, stdout.String(), "Token: configured")
	config, path, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, configPath, path)
	require.Equal(t, Config{Server: server.URL, Token: token, ClientKind: "pactline-cli"}, config)
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestDevelopmentAuthSupportsExplicitUserScopesLifetimeAndClientKind(t *testing.T) {
	const userID = "725f2560-31b5-4bc6-8b8c-57f594b5aca2"
	server := developmentAuthServer(t, func(request developmentTokenRequest) {
		require.Equal(t, developmentTokenRequest{
			Name: "fleet-local", Scopes: []string{"work:read", "work:execute"}, ExpiresInDays: 365,
		}, request)
	})
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "pactline", "config.json"))
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"--json", "--client-kind", "deepseek-fleet", "auth", "development",
		"--server", server.URL, "--user-id", userID, "--token-name", "fleet-local",
		"--scope", "work:read", "--scope", "work:execute", "--expires-in-days", "365",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			UserID        string   `json:"user_id"`
			ClientKind    string   `json:"client_kind"`
			Scopes        []string `json:"scopes"`
			ExpiresInDays int      `json:"expires_in_days"`
			Token         string   `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.True(t, envelope.OK)
	require.Equal(t, userID, envelope.Data.UserID)
	require.Equal(t, "deepseek-fleet", envelope.Data.ClientKind)
	require.Equal(t, []string{"work:read", "work:execute"}, envelope.Data.Scopes)
	require.Equal(t, 365, envelope.Data.ExpiresInDays)
	require.Equal(t, "configured", envelope.Data.Token)
}

func TestDevelopmentAuthRequiresExplicitServerAndAllowedLifetime(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	for _, arguments := range [][]string{
		{"auth", "development"},
		{"auth", "development", "--server", server.URL, "--expires-in-days", "31"},
		{"auth", "development", "--server", server.URL, "--user-id", "not-a-uuid"},
		{"auth", "development", "--server", "http://user:password@localhost:5173"},
	} {
		var stdout, stderr bytes.Buffer
		code := ExecuteArgs(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
		require.Equal(t, 2, code)
		require.Contains(t, stderr.String(), "Error [USAGE]")
	}
	require.Zero(t, requests)
}

func TestDevelopmentTokenLifetimeAllowsOnlyServerSupportedValues(t *testing.T) {
	require.True(t, validDevelopmentTokenLifetime(30))
	require.True(t, validDevelopmentTokenLifetime(90))
	require.True(t, validDevelopmentTokenLifetime(365))
	require.False(t, validDevelopmentTokenLifetime(29))
	require.False(t, validDevelopmentTokenLifetime(366))
}

func TestDevelopmentAuthFailureDoesNotWriteConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/auth/dev/session", request.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"development authentication unavailable"}`))
	}))
	t.Cleanup(server.Close)
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PACTLINE_CONFIG", configPath)
	var stdout, stderr bytes.Buffer

	code := ExecuteArgs(context.Background(), []string{
		"auth", "development", "--server", server.URL,
	}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 5, code)
	require.Contains(t, stderr.String(), "Error [DEVELOPMENT_AUTH_FAILED]: development authentication unavailable")
	_, err := os.Stat(configPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDevelopmentAuthRequiresCSRFCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/auth/dev/session", request.URL.Path)
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "session", Path: "/", HttpOnly: true})
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer

	code := ExecuteArgs(context.Background(), []string{
		"auth", "development", "--server", server.URL,
	}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 5, code)
	require.Contains(t, stderr.String(), "did not provide a CSRF cookie")
}

func developmentAuthServer(t *testing.T, verify func(developmentTokenRequest)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/auth/dev/session":
			http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "session", Path: "/", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "bb_csrf", Value: "csrf", Path: "/"})
			w.WriteHeader(http.StatusNoContent)
		case "/api/account/tokens":
			var body developmentTokenRequest
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			verify(body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"bb_pat_test-secret"}`))
		case "/api/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request path %s", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
