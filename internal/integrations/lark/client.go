package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/identity"
)

const (
	defaultBaseURL          = "https://open.larksuite.com"
	defaultAuthorizationURL = "https://accounts.larksuite.com/open-apis/authen/v1/authorize"
	defaultRequestTimeout   = 10 * time.Second
)

type Config struct {
	AppID            string
	AppSecret        string
	TenantKey        string
	BaseURL          string
	AuthorizationURL string
	RedirectURI      string
	Cipher           *identity.CredentialCipher
	EncryptionKeyID  string
	HTTPClient       *http.Client
}

type Client struct {
	appID, appSecret, tenantKey string
	baseURL, authorizationURL   string
	redirectURI                 string
	cipher                      *identity.CredentialCipher
	encryptionKeyID             string
	httpClient                  *http.Client
	now                         func() time.Time
}

func NewClient(config Config) (*Client, error) {
	if config.AppID == "" || config.AppSecret == "" || config.TenantKey == "" {
		return nil, errors.New("lark app id, app secret, and tenant key are required")
	}
	if config.Cipher == nil || config.EncryptionKeyID == "" {
		return nil, errors.New("lark credential cipher and encryption key id are required")
	}
	if config.RedirectURI == "" {
		return nil, errors.New("Lark redirect URI is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.AuthorizationURL == "" {
		config.AuthorizationURL = defaultAuthorizationURL
	}
	if _, err := url.ParseRequestURI(config.BaseURL); err != nil {
		return nil, fmt.Errorf("parse Lark base URL: %w", err)
	}
	if config.HTTPClient == nil {
		transport := &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		}
		config.HTTPClient = &http.Client{Transport: transport, Timeout: defaultRequestTimeout}
	}
	return &Client{
		appID: config.AppID, appSecret: config.AppSecret, tenantKey: config.TenantKey,
		baseURL: strings.TrimRight(config.BaseURL, "/"), authorizationURL: config.AuthorizationURL,
		redirectURI: config.RedirectURI,
		cipher:      config.Cipher, encryptionKeyID: config.EncryptionKeyID, httpClient: config.HTTPClient,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (c *Client) StartAuthorization(_ context.Context, request identity.AuthorizationRequest) (identity.AuthorizationStart, error) {
	if request.RedirectURI != c.redirectURI {
		return identity.AuthorizationStart{}, providerError(
			"authorization_url", identity.ProviderContract, "", errors.New("redirect URI does not match Lark client configuration"))
	}
	value, err := url.Parse(c.authorizationURL)
	if err != nil {
		return identity.AuthorizationStart{}, providerError("authorization_url", identity.ProviderContract, "", err)
	}
	query := value.Query()
	query.Set("client_id", c.appID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", c.redirectURI)
	query.Set("state", request.State)
	query.Set("scope", strings.Join([]string{
		"auth:user.id:read",
		"contact:contact.base:readonly",
		"contact:user.base:readonly",
		"contact:user.email:readonly",
		"contact:user:search",
		"offline_access",
	}, " "))
	value.RawQuery = query.Encode()
	return identity.AuthorizationStart{URL: value.String()}, nil
}

func (c *Client) ExchangeAuthorizationCode(ctx context.Context, code string) (identity.AuthenticatedPrincipal, error) {
	var tokens tokenResponse
	requestID, err := c.doJSON(ctx, "exchange_authorization_code", http.MethodPost,
		"/open-apis/authen/v2/oauth/token", "", map[string]string{
			"grant_type": "authorization_code", "client_id": c.appID, "client_secret": c.appSecret,
			"code": code, "redirect_uri": c.redirectURI,
		}, &tokens)
	if err != nil {
		return identity.AuthenticatedPrincipal{}, err
	}
	if tokens.Code != 0 {
		return identity.AuthenticatedPrincipal{}, providerError(
			"exchange_authorization_code", classifyOAuthTokenCode(tokens.Code), requestID, fmt.Errorf("provider code %d", tokens.Code))
	}
	if err := validateTokens(tokens); err != nil {
		return identity.AuthenticatedPrincipal{}, providerError(
			"exchange_authorization_code", identity.ProviderContract, requestID, err)
	}
	principal, err := c.getUserInfo(ctx, tokens.AccessToken)
	if err != nil {
		return identity.AuthenticatedPrincipal{}, err
	}
	credential, err := c.sealTokens(tokens)
	if err != nil {
		return identity.AuthenticatedPrincipal{}, providerError("seal_credential", identity.ProviderContract, "", err)
	}
	return identity.AuthenticatedPrincipal{Principal: principal, Credential: credential}, nil
}

func (c *Client) RefreshCredential(ctx context.Context, credential identity.OAuthCredential) (identity.RefreshedCredential, error) {
	refreshToken, err := c.cipher.Decrypt(credential.EncryptionKeyID, credential.RefreshTokenCiphertext)
	if err != nil {
		return identity.RefreshedCredential{}, providerError("decrypt_refresh_credential", identity.ProviderContract, "", err)
	}
	var tokens tokenResponse
	requestID, err := c.doJSON(ctx, "refresh_credential", http.MethodPost,
		"/open-apis/authen/v2/oauth/token", "", map[string]string{
			"grant_type": "refresh_token", "client_id": c.appID, "client_secret": c.appSecret,
			"refresh_token": string(refreshToken),
		}, &tokens)
	if err != nil {
		return identity.RefreshedCredential{}, err
	}
	if tokens.Code != 0 {
		return identity.RefreshedCredential{}, providerError(
			"refresh_credential", classifyOAuthTokenCode(tokens.Code), requestID, fmt.Errorf("provider code %d", tokens.Code))
	}
	if err := validateTokens(tokens); err != nil {
		return identity.RefreshedCredential{}, providerError(
			"refresh_credential", identity.ProviderContract, requestID, err)
	}
	sealed, err := c.sealTokens(tokens)
	if err != nil {
		return identity.RefreshedCredential{}, providerError("seal_refreshed_credential", identity.ProviderContract, "", err)
	}
	return identity.RefreshedCredential{Credential: sealed}, nil
}

func (c *Client) VerifyPrincipal(ctx context.Context, credential identity.OAuthCredential, expected identity.PrincipalKey) (identity.VerificationResult, error) {
	accessToken, err := c.cipher.Decrypt(credential.EncryptionKeyID, credential.AccessTokenCiphertext)
	if err != nil {
		return identity.VerificationResult{}, providerError("decrypt_access_credential", identity.ProviderContract, "", err)
	}
	principal, code, requestID, err := c.requestUserInfo(ctx, string(accessToken))
	if err != nil {
		var providerErr *ProviderError
		if errors.As(err, &providerErr) && (providerErr.Category == identity.ProviderRateLimited ||
			providerErr.Category == identity.ProviderUnavailable) {
			return identity.VerificationResult{
				State: identity.VerificationTransient, Category: providerErr.Category, RequestID: providerErr.RequestID,
			}, nil
		}
		return identity.VerificationResult{}, err
	}
	if code != 0 {
		category := invalidUserInfoCategory(code)
		if category != "" {
			return identity.VerificationResult{
				State: identity.VerificationInvalid, Category: category, RequestID: requestID,
			}, nil
		}
		return identity.VerificationResult{}, providerError("verify_principal", identity.ProviderContract, requestID, fmt.Errorf("unexpected provider code %d", code))
	}
	if principal.Key != expected {
		return identity.VerificationResult{}, providerError("verify_principal", identity.ProviderContract, requestID, errors.New("principal identity mismatch"))
	}
	return identity.VerificationResult{State: identity.VerificationValid, Principal: &principal, RequestID: requestID}, nil
}

func (c *Client) SearchPrincipals(ctx context.Context, credential identity.OAuthCredential, query string, limit int) ([]identity.Principal, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	accessToken, err := c.cipher.Decrypt(credential.EncryptionKeyID, credential.AccessTokenCiphertext)
	if err != nil {
		return nil, providerError("decrypt_directory_credential", identity.ProviderContract, "", err)
	}
	var output []identity.Principal
	pageToken := ""
	for len(output) < limit {
		var response providerEnvelope[searchData]
		parameters := url.Values{}
		parameters.Set("query", query)
		parameters.Set("page_size", fmt.Sprintf("%d", min(20, limit-len(output))))
		if pageToken != "" {
			parameters.Set("page_token", pageToken)
		}
		path := "/open-apis/search/v1/user?" + parameters.Encode()
		requestID, err := c.doJSON(
			ctx, "search_principals", http.MethodGet, path, string(accessToken), nil, &response,
		)
		if err != nil {
			return nil, err
		}
		if response.Code != 0 {
			return nil, providerError("search_principals", classifyProviderCode(response.Code),
				firstNonEmpty(requestID, response.Error.LogID), fmt.Errorf("provider code %d", response.Code))
		}
		users := response.Data.Users
		if len(users) == 0 {
			users = response.Data.Items
		}
		for _, user := range users {
			principal, err := c.normalizeUser(user)
			if err != nil {
				return nil, providerError("search_principals", identity.ProviderContract,
					firstNonEmpty(requestID, response.Error.LogID), err)
			}
			output = append(output, principal)
			if len(output) == limit {
				break
			}
		}
		if !response.Data.HasMore || response.Data.PageToken == "" {
			break
		}
		pageToken = response.Data.PageToken
	}
	return output, nil
}

func (c *Client) GetPrincipal(ctx context.Context, credential identity.OAuthCredential, subjectID string) (identity.Principal, error) {
	accessToken, err := c.cipher.Decrypt(credential.EncryptionKeyID, credential.AccessTokenCiphertext)
	if err != nil {
		return identity.Principal{}, providerError("decrypt_directory_credential", identity.ProviderContract, "", err)
	}
	path := "/open-apis/contact/v3/users/" + url.PathEscape(subjectID) + "?user_id_type=open_id"
	var response providerEnvelope[struct {
		User userInfo `json:"user"`
	}]
	requestID, err := c.doJSON(
		ctx, "get_principal", http.MethodGet, path, string(accessToken), nil, &response,
	)
	if err != nil {
		return identity.Principal{}, err
	}
	if response.Code != 0 {
		return identity.Principal{}, providerError("get_principal", classifyProviderCode(response.Code),
			firstNonEmpty(requestID, response.Error.LogID), fmt.Errorf("provider code %d", response.Code))
	}
	principal, err := c.normalizeUser(response.Data.User)
	if err != nil || principal.Key.SubjectID != subjectID {
		return identity.Principal{}, providerError("get_principal", identity.ProviderContract,
			firstNonEmpty(requestID, response.Error.LogID), errors.New("malformed principal"))
	}
	return principal, nil
}

func (c *Client) SendInvitation(ctx context.Context, recipient identity.PrincipalKey, invitationURL string) (identity.DeliveryReceipt, error) {
	if recipient.Provider != "lark" || recipient.TenantID != c.tenantKey {
		return identity.DeliveryReceipt{}, providerError("send_invitation", identity.ProviderContract, "", errors.New("recipient tenant mismatch"))
	}
	tenantToken, err := c.tenantAccessToken(ctx)
	if err != nil {
		return identity.DeliveryReceipt{}, err
	}
	content, _ := json.Marshal(map[string]string{"text": invitationURL})
	var response providerEnvelope[messageData]
	requestID, err := c.doJSON(ctx, "send_invitation", http.MethodPost,
		"/open-apis/im/v1/messages?receive_id_type=open_id", tenantToken,
		map[string]string{"receive_id": recipient.SubjectID, "msg_type": "text", "content": string(content)}, &response)
	if err != nil {
		return identity.DeliveryReceipt{}, err
	}
	if response.Code != 0 || response.Data.MessageID == "" {
		return identity.DeliveryReceipt{}, providerError("send_invitation", classifyProviderCode(response.Code),
			firstNonEmpty(requestID, response.Error.LogID), fmt.Errorf("provider code %d", response.Code))
	}
	return identity.DeliveryReceipt{ProviderReference: response.Data.MessageID, RequestID: requestID}, nil
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	var response tenantTokenResponse
	requestID, err := c.doJSON(ctx, "tenant_access_token", http.MethodPost,
		"/open-apis/auth/v3/tenant_access_token/internal", "",
		map[string]string{"app_id": c.appID, "app_secret": c.appSecret}, &response)
	if err != nil {
		return "", err
	}
	if response.Code != 0 || response.TenantAccessToken == "" {
		return "", providerError("tenant_access_token", classifyProviderCode(response.Code),
			requestID, fmt.Errorf("provider code %d", response.Code))
	}
	return response.TenantAccessToken, nil
}

func (c *Client) getUserInfo(ctx context.Context, accessToken string) (identity.Principal, error) {
	principal, code, requestID, err := c.requestUserInfo(ctx, accessToken)
	if err != nil {
		return identity.Principal{}, err
	}
	if code != 0 {
		return identity.Principal{}, providerError("user_info", invalidUserInfoCategory(code), requestID, fmt.Errorf("provider code %d", code))
	}
	return principal, nil
}

func (c *Client) requestUserInfo(ctx context.Context, accessToken string) (identity.Principal, int, string, error) {
	var response providerEnvelope[userInfo]
	requestID, err := c.doJSON(ctx, "user_info", http.MethodGet, "/open-apis/authen/v1/user_info", accessToken, nil, &response)
	if err != nil {
		return identity.Principal{}, 0, requestID, err
	}
	if response.Code != 0 {
		return identity.Principal{}, response.Code, firstNonEmpty(requestID, response.Error.LogID), nil
	}
	principal, err := c.normalizeUser(response.Data)
	if err != nil {
		return identity.Principal{}, 0, requestID, providerError("user_info", identity.ProviderContract, requestID, err)
	}
	return principal, 0, requestID, nil
}

func (c *Client) normalizeUser(user userInfo) (identity.Principal, error) {
	if user.OpenID == "" || user.Name == "" {
		return identity.Principal{}, errors.New("provider principal is incomplete")
	}
	if user.TenantKey != "" && user.TenantKey != c.tenantKey {
		return identity.Principal{}, errors.New("provider tenant mismatch")
	}
	var email, avatar *string
	if user.Email != "" {
		email = &user.Email
	}
	avatarURL := firstNonEmpty(
		user.Avatar.Avatar240,
		user.Avatar.AvatarOrigin,
		user.AvatarURL,
		user.Avatar.Avatar640,
		user.Avatar.Avatar72,
	)
	if avatarURL != "" {
		avatar = &avatarURL
	}
	profile, _ := json.Marshal(map[string]any{
		"name": user.Name, "email": user.Email, "avatar_url": avatarURL,
	})
	return identity.Principal{
		Key:  identity.PrincipalKey{Provider: "lark", TenantID: c.tenantKey, SubjectID: user.OpenID},
		Name: user.Name, Email: email, EmailVerified: email != nil, AvatarURL: avatar,
		Active: !user.Status.IsResigned && !user.Status.IsFrozen, Profile: profile,
	}, nil
}

func (c *Client) sealTokens(tokens tokenResponse) (identity.OAuthCredential, error) {
	access, err := c.cipher.Encrypt(c.encryptionKeyID, []byte(tokens.AccessToken))
	if err != nil {
		return identity.OAuthCredential{}, err
	}
	refresh, err := c.cipher.Encrypt(c.encryptionKeyID, []byte(tokens.RefreshToken))
	if err != nil {
		return identity.OAuthCredential{}, err
	}
	now := c.now()
	return identity.OAuthCredential{
		AccessTokenCiphertext: access, RefreshTokenCiphertext: refresh,
		AccessTokenExpiresAt:  now.Add(time.Duration(tokens.ExpiresIn) * time.Second),
		RefreshTokenExpiresAt: now.Add(time.Duration(tokens.RefreshTokenExpiresIn) * time.Second),
		EncryptionKeyID:       c.encryptionKeyID,
	}, nil
}

func (c *Client) doJSON(ctx context.Context, operation, method, path, token string, input, output any) (string, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return "", providerError(operation, identity.ProviderContract, "", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return "", providerError(operation, identity.ProviderContract, "", err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		slog.Warn("Lark request failed", "operation", operation, "error_category", identity.ProviderUnavailable)
		return "", providerError(operation, identity.ProviderUnavailable, "", err)
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Tt-Logid")
	if response.StatusCode == http.StatusTooManyRequests {
		return requestID, providerError(operation, identity.ProviderRateLimited, requestID, errors.New("provider rate limited"))
	}
	if response.StatusCode >= 500 {
		return requestID, providerError(operation, identity.ProviderUnavailable, requestID, errors.New("provider unavailable"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return requestID, providerError(operation, identity.ProviderUnauthorized, requestID, fmt.Errorf("provider HTTP status %d", response.StatusCode))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(output); err != nil {
		return requestID, providerError(operation, identity.ProviderContract, requestID, err)
	}
	return requestID, nil
}

func validateTokens(tokens tokenResponse) error {
	if tokens.Code != 0 || tokens.AccessToken == "" || tokens.RefreshToken == "" ||
		tokens.ExpiresIn <= 0 || tokens.RefreshTokenExpiresIn <= 0 {
		return fmt.Errorf("invalid OAuth token response code %d", tokens.Code)
	}
	return nil
}

func invalidUserInfoCategory(code int) identity.ProviderErrorCategory {
	switch code {
	case 20005:
		return identity.ProviderUnauthorized
	case 20008, 20023:
		return identity.ProviderNotFound
	case 20021:
		return identity.ProviderResigned
	case 20022:
		return identity.ProviderFrozen
	default:
		return ""
	}
}

func classifyProviderCode(code int) identity.ProviderErrorCategory {
	switch code {
	case 20005, 99991663, 99991668, 99991677:
		return identity.ProviderUnauthorized
	case 99991400:
		return identity.ProviderRateLimited
	default:
		return identity.ProviderContract
	}
}

func classifyOAuthTokenCode(code int) identity.ProviderErrorCategory {
	switch code {
	case 20026, 20037:
		return identity.ProviderCredentialExpired
	case 20064, 20073:
		return identity.ProviderAuthorizationRevoked
	case 20005, 99991663, 99991668, 99991677:
		return identity.ProviderUnauthorized
	case 99991400:
		return identity.ProviderRateLimited
	default:
		return identity.ProviderContract
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
