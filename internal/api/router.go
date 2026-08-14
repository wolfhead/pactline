package api

import (
	"net/http"
	"net/url"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/notification"
)

type AuthSurface struct {
	Sessions      *identity.Service
	Tokens        *access.Service
	Delegates     *access.DelegateService
	AccessAudit   accessAuditStore
	LarkAudit     larkAuditReader
	Idempotency   idempotencyRepository
	Development   developmentAuthenticator
	LarkEnabled   bool
	AppBaseURL    *url.URL
	SecureCookies bool
}

type RouterOptions struct {
	Auth                       AuthSurface
	V1                         http.Handler
	OpenAPI                    http.Handler
	AgentStatus                channel.StatusProvider
	AdminTools                 *notification.TestService
	AdminRepositoryConnections *application.RepositoryConnectionService
}

type accessAuditStore interface {
	accessAuditWriter
	accessAuditReader
}

// NewRouter builds the top-level API handler. Work-management resources are
// exposed only through the contract-first /api/v1 handler. Authentication,
// account administration, and the preserved legacy mechanism retain their
// internal route families and the same session boundary.
func NewRouter(
	legacyMux http.Handler,
	options RouterOptions,
) http.Handler {
	protected := http.NewServeMux()

	protected.Handle("/api/legacy/", legacyMux)

	cookies := cookieSettings{secure: options.Auth.SecureCookies}
	auth := &authHandler{
		sessions: options.Auth.Sessions, development: options.Auth.Development, cookies: cookies,
	}
	protected.HandleFunc("GET /api/me", auth.me)
	protected.HandleFunc("POST /api/auth/logout", auth.logout)
	adminIdentity := &adminIdentityHandler{service: options.Auth.Sessions}
	protected.HandleFunc("GET /api/admin/directory/search", adminIdentity.searchDirectory)
	protected.HandleFunc("GET /api/admin/invitations", adminIdentity.listInvitations)
	protected.HandleFunc("POST /api/admin/invitations", adminIdentity.createInvitation)
	protected.HandleFunc("POST /api/admin/invitations/{id}/resend", adminIdentity.resendInvitation)
	protected.HandleFunc("POST /api/admin/invitations/{id}/link", adminIdentity.rotateInvitationLink)
	protected.HandleFunc("DELETE /api/admin/invitations/{id}", adminIdentity.revokeInvitation)
	protected.HandleFunc("GET /api/admin/users", adminIdentity.listUsers)
	protected.HandleFunc("PATCH /api/admin/users/{id}", adminIdentity.updateUser)
	protected.HandleFunc("POST /api/admin/impersonation", adminIdentity.startImpersonation)
	protected.HandleFunc("DELETE /api/admin/impersonation", adminIdentity.endImpersonation)
	accountAccess := &accountTokenHandler{
		tokens: options.Auth.Tokens, audit: options.Auth.AccessAudit,
	}
	protected.HandleFunc("GET /api/account/tokens", accountAccess.list)
	protected.HandleFunc("POST /api/account/tokens", accountAccess.create)
	protected.HandleFunc("DELETE /api/account/tokens/{id}", accountAccess.revoke)
	protected.HandleFunc("GET /api/account/api-activity", accountAccess.activity)
	adminAccess := &adminAccessHandler{
		tokens: options.Auth.Tokens, audit: options.Auth.AccessAudit,
		lark: options.Auth.LarkAudit,
	}
	protected.HandleFunc("GET /api/admin/api-tokens", adminAccess.listTokens)
	protected.HandleFunc("DELETE /api/admin/api-tokens/{id}", adminAccess.revokeToken)
	protected.HandleFunc("GET /api/admin/api-activity", adminAccess.activity)
	protected.HandleFunc("GET /api/admin/lark-api-activity", adminAccess.larkActivity)
	if options.AgentStatus != nil {
		agentStatus := &agentStatusHandler{status: options.AgentStatus}
		protected.HandleFunc("GET /api/admin/agent/status", agentStatus.get)
	}
	if options.AdminTools != nil {
		adminTools := &adminToolsHandler{notifications: options.AdminTools}
		protected.HandleFunc("GET /api/admin/tools/notifications/recipients", adminTools.listNotificationRecipients)
		protected.HandleFunc("POST /api/admin/tools/notifications/test", adminTools.requestDMTest)
	}
	if options.AdminRepositoryConnections != nil {
		adminConnections := &adminRepositoryConnectionHandler{connections: options.AdminRepositoryConnections}
		protected.HandleFunc("GET /api/admin/repository-connections", adminConnections.list)
		protected.HandleFunc("POST /api/admin/repository-connections", adminConnections.create)
		protected.HandleFunc("PATCH /api/admin/repository-connections/{id}/credential", adminConnections.rotateCredential)
		protected.HandleFunc("POST /api/admin/repository-connections/{id}/validate", adminConnections.validate)
		protected.HandleFunc("POST /api/admin/repository-connections/{id}/disable", adminConnections.disable)
	}

	root := http.NewServeMux()
	if options.Auth.Development != nil {
		root.HandleFunc("POST /api/auth/dev/session", auth.developmentSession)
	}
	if options.Auth.LarkEnabled {
		root.HandleFunc("GET /api/auth/lark/start", auth.larkStart)
		root.HandleFunc("GET /api/auth/lark/callback", auth.larkCallback)
		root.HandleFunc("POST /api/invitations/accept", adminIdentity.acceptInvitation)
	}
	middleware := identityMiddleware{
		sessions: options.Auth.Sessions, appBaseURL: options.Auth.AppBaseURL, cookies: cookies,
		routes: protected,
	}
	v1 := options.V1
	if v1 == nil {
		v1 = http.NotFoundHandler()
	}
	resolver, _ := v1.(routeResolver)
	v1Session := identityMiddleware{
		sessions: options.Auth.Sessions, appBaseURL: options.Auth.AppBaseURL, cookies: cookies,
		routes: resolver,
	}.wrap(v1)
	limiter := newTokenBucketLimiter()
	v1Bearer := idempotencyMiddleware{
		store: options.Auth.Idempotency, routes: resolver,
	}.wrap(v1)
	v1Audited := apiAccessAudit{
		store: options.Auth.AccessAudit, routes: resolver,
	}.wrap(bearerAuthentication{
		tokens: access.BearerService{
			Tokens: options.Auth.Tokens, Delegates: options.Auth.Delegates,
		},
		owners:  options.Auth.Sessions,
		limiter: limiter,
	}.wrap(v1Bearer, v1Session))
	root.Handle("/api/v1", v1Audited)
	root.Handle("/api/v1/", v1Audited)
	if options.OpenAPI != nil {
		document := RequireWorkRead(options.OpenAPI)
		documentSession := identityMiddleware{
			sessions: options.Auth.Sessions, appBaseURL: options.Auth.AppBaseURL, cookies: cookies,
		}.wrap(document)
		documentAuthenticated := bearerAuthentication{
			tokens: options.Auth.Tokens, owners: options.Auth.Sessions, limiter: limiter,
		}.wrap(document, documentSession)
		root.Handle("/api/openapi.yaml", documentAuthenticated)
	}
	root.Handle("/", middleware.wrap(protected))
	return RequestIDMiddleware(isolateBearerFromInternal(root))
}
