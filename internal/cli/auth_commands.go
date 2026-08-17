package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	primaryDevelopmentUserID = "00000000-0000-0000-0000-000000000001"
	developmentTokenName     = "pactline-cli-development"
)

func (a *App) authCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate the CLI",
	}
	command.AddCommand(a.developmentAuthCommand())
	return command
}

func (a *App) developmentAuthCommand() *cobra.Command {
	var userID, tokenName string
	var scopes []string
	var expiresInDays int
	command := &cobra.Command{
		Use:   "development",
		Short: "Create and securely store a Token through development authentication",
		Long: "Create a development browser Session for a local seed user, exchange it for a personal API Token, " +
			"and store the Token in the CLI config. This command does not accept a password and fails when development authentication is disabled.",
		Example: `pactline auth development --server http://localhost:5173`,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !command.Flags().Changed("server") || strings.TrimSpace(a.server) == "" {
				return &APIError{Code: "USAGE", Message: "--server is required and must be specified explicitly"}
			}
			server, err := parseDevelopmentServer(a.server)
			if err != nil {
				return err
			}
			if _, err := uuid.Parse(userID); err != nil {
				return &APIError{Code: "USAGE", Message: "--user-id must be a UUID"}
			}
			if strings.TrimSpace(tokenName) == "" {
				return &APIError{Code: "USAGE", Message: "--token-name must not be empty"}
			}
			if len(scopes) == 0 {
				return &APIError{Code: "USAGE", Message: "at least one --scope is required"}
			}
			if !validDevelopmentTokenLifetime(expiresInDays) {
				return &APIError{Code: "USAGE", Message: "--expires-in-days must be one of 30, 90, or 365"}
			}
			clientKind := strings.TrimSpace(a.clientKind)
			if clientKind == "" {
				clientKind = "pactline-cli"
			}

			issued, err := a.issueDevelopmentToken(command.Context(), server, developmentTokenRequest{
				Name: tokenName, Scopes: scopes, ExpiresInDays: expiresInDays,
			}, userID)
			if err != nil {
				return err
			}
			path, err := saveConfig(Config{
				Server: strings.TrimRight(server.String(), "/"), Token: issued.Token, ClientKind: clientKind,
			})
			if err != nil {
				return &APIError{Code: "CONFIG_ERROR", Message: err.Error()}
			}
			data := map[string]any{
				"path": path, "server": strings.TrimRight(server.String(), "/"), "user_id": userID,
				"client_kind": clientKind, "scopes": scopes, "expires_in_days": expiresInDays, "token": "configured",
			}
			return a.output(data, func(w io.Writer) {
				fmt.Fprintf(w, "Saved: %s\nServer: %s\nUser ID: %s\nClient kind: %s\nScopes: %s\nExpires in days: %d\nToken: configured\n",
					path, strings.TrimRight(server.String(), "/"), userID, clientKind, strings.Join(scopes, ","), expiresInDays)
			})
		},
	}
	command.Flags().StringVar(&userID, "user-id", primaryDevelopmentUserID, "development seed user UUID")
	command.Flags().StringVar(&tokenName, "token-name", developmentTokenName, "personal Token name")
	command.Flags().StringSliceVar(&scopes, "scope", []string{"work:execute"}, "Token scope (repeat for multiple scopes)")
	command.Flags().IntVar(&expiresInDays, "expires-in-days", 30, "Token lifetime: 30, 90, or 365 days")
	return command
}

type developmentTokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays int      `json:"expires_in_days"`
}

type developmentTokenResponse struct {
	Token string `json:"token"`
}

func parseDevelopmentServer(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, &APIError{Code: "USAGE", Message: "--server must be an absolute HTTP(S) URL"}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func validDevelopmentTokenLifetime(days int) bool {
	return days == 30 || days == 90 || days == 365
}

func (a *App) issueDevelopmentToken(
	ctx context.Context,
	server *url.URL,
	tokenRequest developmentTokenRequest,
	userID string,
) (developmentTokenResponse, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return developmentTokenResponse{}, fmt.Errorf("create development authentication cookie jar: %w", err)
	}
	httpClient := &http.Client{
		Jar: jar, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if err := a.developmentAuthRequest(ctx, httpClient, server, "/api/auth/dev/session", map[string]string{"user_id": userID}); err != nil {
		return developmentTokenResponse{}, err
	}

	csrfToken := ""
	for _, cookie := range jar.Cookies(server) {
		if cookie.Name == "bb_csrf" {
			csrfToken = cookie.Value
			break
		}
	}
	if csrfToken == "" {
		return developmentTokenResponse{}, &APIError{
			Code: "DEVELOPMENT_AUTH_FAILED", Message: "development session did not provide a CSRF cookie",
		}
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.logoutDevelopmentSession(cleanupContext, httpClient, server, csrfToken); err != nil {
			a.debugf("development authentication cleanup failed: %s", err)
		}
	}()
	body, err := json.Marshal(tokenRequest)
	if err != nil {
		return developmentTokenResponse{}, fmt.Errorf("encode development Token request: %w", err)
	}
	endpoint, err := url.JoinPath(server.String(), "/api/account/tokens")
	if err != nil {
		return developmentTokenResponse{}, fmt.Errorf("build development Token URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return developmentTokenResponse{}, fmt.Errorf("build development Token request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.Scheme+"://"+server.Host)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-CSRF-Token", csrfToken)
	a.debugf("development authentication request method=POST path=/api/account/tokens")
	response, err := httpClient.Do(request)
	if err != nil {
		return developmentTokenResponse{}, &APIError{Code: "NETWORK_ERROR", Message: "development Token request failed: " + err.Error()}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return developmentTokenResponse{}, fmt.Errorf("read development Token response: %w", err)
	}
	a.debugf("development authentication response status=%d path=/api/account/tokens", response.StatusCode)
	if response.StatusCode != http.StatusCreated {
		return developmentTokenResponse{}, developmentAuthHTTPError(response.StatusCode, responseBody)
	}
	var issued developmentTokenResponse
	if err := json.Unmarshal(responseBody, &issued); err != nil {
		return developmentTokenResponse{}, fmt.Errorf("decode development Token response: %w", err)
	}
	if strings.TrimSpace(issued.Token) == "" {
		return developmentTokenResponse{}, &APIError{Code: "DEVELOPMENT_AUTH_FAILED", Message: "development Token response did not contain a Token"}
	}
	return issued, nil
}

func (a *App) logoutDevelopmentSession(
	ctx context.Context,
	httpClient *http.Client,
	server *url.URL,
	csrfToken string,
) error {
	endpoint, err := url.JoinPath(server.String(), "/api/auth/logout")
	if err != nil {
		return fmt.Errorf("build development logout URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build development logout request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", server.Scheme+"://"+server.Host)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-CSRF-Token", csrfToken)
	a.debugf("development authentication request method=POST path=/api/auth/logout")
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("development logout request failed: %w", err)
	}
	defer response.Body.Close()
	a.debugf("development authentication response status=%d path=/api/auth/logout", response.StatusCode)
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("development logout returned status %d", response.StatusCode)
	}
	return nil
}

func (a *App) developmentAuthRequest(
	ctx context.Context,
	httpClient *http.Client,
	server *url.URL,
	path string,
	payload any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode development authentication request: %w", err)
	}
	endpoint, err := url.JoinPath(server.String(), path)
	if err != nil {
		return fmt.Errorf("build development authentication URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build development authentication request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	a.debugf("development authentication request method=POST path=%s", path)
	response, err := httpClient.Do(request)
	if err != nil {
		return &APIError{Code: "NETWORK_ERROR", Message: "development authentication request failed: " + err.Error()}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return fmt.Errorf("read development authentication response: %w", err)
	}
	a.debugf("development authentication response status=%d path=%s", response.StatusCode, path)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return developmentAuthHTTPError(response.StatusCode, responseBody)
	}
	return nil
}

func developmentAuthHTTPError(status int, body []byte) error {
	response := struct {
		Error string `json:"error"`
	}{}
	_ = json.Unmarshal(body, &response)
	message := strings.TrimSpace(response.Error)
	if message == "" {
		message = http.StatusText(status)
	}
	return &APIError{Status: status, Code: "DEVELOPMENT_AUTH_FAILED", Message: message}
}
