package lark

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"bountyboard/internal/identity"

	"github.com/stretchr/testify/require"
)

func TestAuthorizationAndOAuthTransport(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/open-apis/authen/v2/oauth/token":
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			require.Empty(t, r.Header.Get("Authorization"))
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if body["grant_type"] == "authorization_code" {
				require.Equal(t, "code-secret", body["code"])
				_, _ = w.Write([]byte(`{"code":0,"access_token":"access-secret","expires_in":7200,"refresh_token":"refresh-secret","refresh_token_expires_in":604800}`))
			} else {
				require.Equal(t, "refresh_token", body["grant_type"])
				require.Equal(t, "refresh-secret", body["refresh_token"])
				_, _ = w.Write([]byte(`{"code":0,"access_token":"rotated-access","expires_in":7200,"refresh_token":"rotated-refresh","refresh_token_expires_in":604800}`))
			}
		case "/open-apis/authen/v1/user_info":
			require.Equal(t, "Bearer access-secret", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"data":{"open_id":"ou_1","tenant_key":"tenant","name":"Ada","email":"ada@example.test","avatar_url":"https://avatar.test/1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	start, err := client.StartAuthorization(context.Background(), identity.AuthorizationRequest{
		State: "state value", RedirectURI: "https://app.example.test/api/auth/lark/callback",
	})
	require.NoError(t, err)
	authorizationURL, err := url.Parse(start.URL)
	require.NoError(t, err)
	require.Equal(t, "accounts.example.test", authorizationURL.Host)
	require.Equal(t, "cli_test", authorizationURL.Query().Get("app_id"))
	require.Equal(t, "state value", authorizationURL.Query().Get("state"))
	require.Contains(t, authorizationURL.Query().Get("scope"), "offline_access")

	authenticated, err := client.ExchangeAuthorizationCode(context.Background(), "code-secret")
	require.NoError(t, err)
	require.Equal(t, identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_1"}, authenticated.Principal.Key)
	require.True(t, authenticated.Principal.EmailVerified)
	require.NotContains(t, string(authenticated.Credential.AccessTokenCiphertext), "access-secret")

	refreshed, err := client.RefreshCredential(context.Background(), authenticated.Credential)
	require.NoError(t, err)
	require.NotEmpty(t, refreshed.Credential.AccessTokenCiphertext)
	require.Equal(t, []string{
		"POST /open-apis/authen/v2/oauth/token",
		"GET /open-apis/authen/v1/user_info",
		"POST /open-apis/authen/v2/oauth/token",
	}, requests)
}

func TestSearchPaginationAndDirectMessage(t *testing.T) {
	searchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/search/v1/user":
			searchCalls++
			require.Equal(t, "Bearer access-secret", r.Header.Get("Authorization"))
			if searchCalls == 1 {
				_, _ = w.Write([]byte(`{"code":0,"data":{"users":[{"open_id":"ou_1","name":"Ada"}],"has_more":true,"page_token":"next"}}`))
			} else {
				_, _ = w.Write([]byte(`{"code":0,"data":{"users":[{"open_id":"ou_2","name":"Alan"}],"has_more":false}}`))
			}
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-secret","expire":7200}`))
		case "/open-apis/im/v1/messages":
			require.Equal(t, "open_id", r.URL.Query().Get("receive_id_type"))
			require.Equal(t, "Bearer tenant-secret", r.Header.Get("Authorization"))
			w.Header().Set("X-Tt-Logid", "message-log-id")
			_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	credential := sealTestCredential(t, client)

	results, err := client.SearchPrincipals(context.Background(), credential, "ad", 20)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, 2, searchCalls)

	receipt, err := client.SendInvitation(context.Background(),
		identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_2"},
		"https://app.example.test/invite#secret")
	require.NoError(t, err)
	require.Equal(t, "om_1", receipt.ProviderReference)
	require.Equal(t, "message-log-id", receipt.RequestID)
}

func TestSearchRejectsMalformedSuccessEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"users":[{"open_id":"","name":"Ada"}]}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	_, err := client.SearchPrincipals(context.Background(), sealTestCredential(t, client), "ad", 20)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, identity.ProviderContract, providerErr.Category)
}

func TestProviderClassificationsAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantState  identity.VerificationState
		wantCat    identity.ProviderErrorCategory
		wantErrCat identity.ProviderErrorCategory
	}{
		{name: "missing", body: `{"code":20008,"msg":"missing"}`, wantState: identity.VerificationInvalid, wantCat: identity.ProviderNotFound},
		{name: "resigned", body: `{"code":20021}`, wantState: identity.VerificationInvalid, wantCat: identity.ProviderResigned},
		{name: "frozen", body: `{"code":20022}`, wantState: identity.VerificationInvalid, wantCat: identity.ProviderFrozen},
		{name: "unregistered", body: `{"code":20023}`, wantState: identity.VerificationInvalid, wantCat: identity.ProviderNotFound},
		{name: "unauthorized", body: `{"code":20005}`, wantState: identity.VerificationInvalid, wantCat: identity.ProviderUnauthorized},
		{name: "rate limited", status: http.StatusTooManyRequests, wantState: identity.VerificationTransient, wantCat: identity.ProviderRateLimited},
		{name: "unavailable", status: http.StatusBadGateway, wantState: identity.VerificationTransient, wantCat: identity.ProviderUnavailable},
		{name: "malformed", body: `{`, wantErrCat: identity.ProviderContract},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Tt-Logid", "log-id")
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			result, err := client.VerifyPrincipal(context.Background(), sealTestCredential(t, client),
				identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_1"})
			if tc.wantErrCat != "" {
				var providerErr *ProviderError
				require.ErrorAs(t, err, &providerErr)
				require.Equal(t, tc.wantErrCat, providerErr.Category)
				require.Equal(t, "log-id", providerErr.RequestID)
				require.NotContains(t, err.Error(), "access-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantState, result.State)
			require.Equal(t, tc.wantCat, result.Category)
			require.Equal(t, "log-id", result.RequestID)
		})
	}
}

func TestTenantMismatchAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "user_info") {
			_, _ = w.Write([]byte(`{"code":0,"data":{"open_id":"ou_1","tenant_key":"other","name":"Ada"}}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	_, err := client.VerifyPrincipal(context.Background(), sealTestCredential(t, client),
		identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_1"})
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, identity.ProviderContract, providerErr.Category)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	client = newTestClient(t, slow.URL)
	client.httpClient.Timeout = 10 * time.Millisecond
	result, err := client.VerifyPrincipal(context.Background(), sealTestCredential(t, client),
		identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_1"})
	require.NoError(t, err)
	require.Equal(t, identity.VerificationTransient, result.State)
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	cipher, err := identity.NewCredentialCipher(map[string][]byte{"test": make([]byte, 32)})
	require.NoError(t, err)
	client, err := NewClient(Config{
		AppID: "cli_test", AppSecret: "app-secret", TenantKey: "tenant",
		BaseURL: baseURL, AuthorizationURL: "https://accounts.example.test/open-apis/authen/v1/authorize",
		Cipher: cipher, EncryptionKeyID: "test", HTTPClient: &http.Client{Timeout: time.Second},
	})
	require.NoError(t, err)
	return client
}

func sealTestCredential(t *testing.T, client *Client) identity.OAuthCredential {
	t.Helper()
	credential, err := client.sealTokens(tokenResponse{
		AccessToken: "access-secret", RefreshToken: "refresh-secret",
		ExpiresIn: 3600, RefreshTokenExpiresIn: 7200,
	})
	require.NoError(t, err)
	return credential
}

func TestProviderErrorSupportsErrorsIs(t *testing.T) {
	err := &ProviderError{Category: identity.ProviderRateLimited}
	require.True(t, errors.Is(err, identity.ErrProviderTransient))
}

func TestRefreshClassifiesRevokedCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":20064,"msg":"revoked"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	_, err := client.RefreshCredential(context.Background(), sealTestCredential(t, client))
	category, ok := identity.ProviderCategoryFromError(err)
	require.True(t, ok)
	require.Equal(t, identity.ProviderAuthorizationRevoked, category)
}
