// Package openapi owns the first-party Agent's generated /api/v1 client
// boundary. The in-process transport still executes the complete HTTP router,
// including delegated authentication, scope, idempotency, and audit middleware.
package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/wolfhead/pactline/internal/access"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
	"github.com/ogen-go/ogen/ogenerrors"
)

type CredentialIssuer interface {
	Issue(context.Context, uuid.UUID, uuid.UUID) (string, error)
}

type Factory struct {
	issuer  CredentialIssuer
	handler http.Handler
}

func NewFactory(issuer CredentialIssuer, handler http.Handler) (*Factory, error) {
	if issuer == nil || handler == nil {
		return nil, fmt.Errorf("configure Agent OpenAPI client: issuer and handler are required")
	}
	return &Factory{issuer: issuer, handler: handler}, nil
}

func (f *Factory) New(runID, userID uuid.UUID) (*generated.Client, error) {
	security := &delegateSecurity{
		issuer: f.issuer,
		runID:  runID,
		userID: userID,
	}
	httpClient := &http.Client{Transport: inProcessTransport{handler: f.handler}}
	client, err := generated.NewClient(
		"http://pactline-agent.internal",
		security,
		generated.WithClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("construct Agent OpenAPI client: %w", err)
	}
	return client, nil
}

type delegateSecurity struct {
	issuer CredentialIssuer
	runID  uuid.UUID
	userID uuid.UUID
}

func (s *delegateSecurity) BearerAuth(
	ctx context.Context,
	_ generated.OperationName,
) (generated.BearerAuth, error) {
	credential, err := s.issuer.Issue(ctx, s.runID, s.userID)
	if err != nil {
		return generated.BearerAuth{}, err
	}
	return generated.BearerAuth{Token: credential}, nil
}

func (*delegateSecurity) SessionCookie(
	context.Context,
	generated.OperationName,
) (generated.SessionCookie, error) {
	return generated.SessionCookie{}, ogenerrors.ErrSkipClientSecurity
}

type inProcessTransport struct {
	handler http.Handler
}

func (t inProcessTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "pactline-agent.internal" {
		return nil, fmt.Errorf("Agent OpenAPI transport rejected host %q", request.URL.Host)
	}
	recorder := httptest.NewRecorder()
	request.RemoteAddr = "127.0.0.1:0"
	request.Header.Set("User-Agent", "pactline-first-party-agent")
	t.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	response.Request = request
	if response.Body == nil {
		response.Body = io.NopCloser(http.NoBody)
	}
	return response, nil
}

var _ generated.SecuritySource = (*delegateSecurity)(nil)
var _ http.RoundTripper = inProcessTransport{}
var _ CredentialIssuer = (*access.DelegateService)(nil)
